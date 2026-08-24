// Package miniostore là provider `managed` — lưu trên hạ tầng của ta, dùng MinIO
// (hoặc bất kỳ dịch vụ tương thích S3 nào).
//
// Đây là provider duy nhất mà ta cưỡng chế được hạn mức, và cũng là provider duy
// nhất khiến việc bán dung lượng trở thành dịch vụ số chịu hoa hồng Apple. Xem
// docs/adr/0002-auth-and-storage.md.
package miniostore

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/hauph/camera/backend/internal/storage"
)

const (
	// uploadTTL ngắn: URL upload bị lộ ra log hay ảnh chụp màn hình vẫn là quyền
	// ghi vào không gian của người dùng. Đủ dài cho một file RAW 60MB qua mạng
	// chậm, không dài hơn.
	uploadTTL = 30 * time.Minute
	// downloadTTL dài hơn vì URL đọc thường được nhúng vào giao diện và người
	// dùng có thể lướt lại sau một lúc.
	downloadTTL = 2 * time.Hour
)

var ErrUnsafeKey = errors.New("khoá đối tượng không hợp lệ")

// QuotaFunc trả hạn mức hiện tại của người dùng. Thường là billing.Service.QuotaBytes.
type QuotaFunc func(ctx context.Context, userID string) (int64, error)

// UsageRepo theo dõi dung lượng đã dùng.
//
// Cố ý KHÔNG tính bằng cách liệt kê toàn bộ object mỗi lần: một người dùng có
// hàng chục nghìn file, và liệt kê chúng chỉ để trả lời "còn bao nhiêu chỗ" là
// một truy vấn đắt chạy ở đường nóng.
type UsageRepo interface {
	Used(ctx context.Context, userID string) (int64, error)
	Add(ctx context.Context, userID string, delta int64) error
}

type Store struct {
	client *minio.Client
	bucket string
	quota  QuotaFunc
	usage  UsageRepo
}

type Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

func New(cfg Config, quota QuotaFunc, usage UsageRepo) (*Store, error) {
	c, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("kết nối MinIO: %w", err)
	}
	return &Store{client: c, bucket: cfg.Bucket, quota: quota, usage: usage}, nil
}

// EnsureBucket tạo bucket nếu chưa có. Gọi lúc khởi động.
func (s *Store) EnsureBucket(ctx context.Context) error {
	ok, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("kiểm tra bucket: %w", err)
	}
	if !ok {
		if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("tạo bucket: %w", err)
		}
	}
	return nil
}

func (s *Store) ID() storage.ProviderID { return storage.ProviderManaged }

func (s *Store) Capabilities() []storage.Capability {
	return []storage.Capability{
		storage.CapServerSideRender,
		storage.CapEnforcedQuota,
		storage.CapDurable,
	}
}

// objectKey đặt mọi file của một người dùng dưới tiền tố riêng.
//
// Đây là ranh giới cách ly giữa các người dùng ở tầng lưu trữ, và nó quan trọng
// vì presigned URL không kiểm tra gì ngoài chữ ký — một khi URL đã cấp, storage
// không hỏi lại người gọi là ai.
func objectKey(userID, key string) (string, error) {
	if userID == "" {
		return "", fmt.Errorf("%w: thiếu userID", ErrUnsafeKey)
	}

	key = strings.TrimSpace(key)

	// Kiểm tra ".." trên khoá GỐC, trước khi path.Clean chạm vào.
	//
	// Cách viết trực giác là clean trước rồi tìm ".." trong kết quả — nhưng
	// path.Clean("/../x") trả về "/x", tức là ".." đã bị triệt tiêu và phép kiểm
	// tra không bao giờ thấy gì. Ở đây điều đó không tạo lỗ hổng thoát namespace
	// vì tiền tố users/<id>/ vẫn được ghép vào, nhưng nó ÂM THẦM VIẾT LẠI khoá
	// của client: client xin ghi vào "../a/b.NEF" và nhận về "users/u1/a/b.NEF",
	// rồi sau đó không tìm lại được file của chính mình.
	//
	// Khoá chứa ".." nghĩa là có lỗi ở phía client hoặc có người đang thử thoát
	// thư mục. Cả hai đều phải dừng lại và báo, không phải im lặng sửa hộ.
	for _, seg := range strings.Split(key, "/") {
		if seg == ".." {
			return "", fmt.Errorf("%w: chứa \"..\": %q", ErrUnsafeKey, key)
		}
	}

	cleaned := path.Clean("/" + key)
	if cleaned == "/" {
		return "", fmt.Errorf("%w: khoá rỗng", ErrUnsafeKey)
	}
	return "users/" + userID + cleaned, nil
}

// Upload cấp URL presigned để client tự tải lên.
//
// CẢNH BÁO về giới hạn của presigned PUT: chữ ký KHÔNG ràng buộc kích thước.
// Client khai 1MB rồi tải lên 10GB thì S3 vẫn nhận. Vì vậy kiểm tra hạn mức ở
// đây chỉ là lớp phòng thủ THỨ NHẤT và mang tính tư vấn — nó chặn được người
// dùng trung thực khỏi bắt đầu một upload sẽ thất bại, nhưng không chặn được kẻ
// cố tình.
//
// Lớp phòng thủ THẬT nằm ở Confirm: sau khi tải lên, server hỏi kích thước THẬT
// từ storage và xoá đối tượng nếu vượt hạn mức. Xem Confirm.
func (s *Store) Upload(ctx context.Context, userID, key string, size int64) (storage.Target, error) {
	obj, err := objectKey(userID, key)
	if err != nil {
		return storage.Target{}, err
	}

	if size > 0 {
		limit, err := s.quota(ctx, userID)
		if err != nil {
			return storage.Target{}, fmt.Errorf("đọc hạn mức: %w", err)
		}
		used, err := s.usage.Used(ctx, userID)
		if err != nil {
			return storage.Target{}, fmt.Errorf("đọc dung lượng đã dùng: %w", err)
		}
		if used+size > limit {
			return storage.Target{}, fmt.Errorf("%w: cần %d byte, còn %d",
				storage.ErrQuotaExceeded, size, limit-used)
		}
	}

	u, err := s.client.PresignedPutObject(ctx, s.bucket, obj, uploadTTL)
	if err != nil {
		return storage.Target{}, fmt.Errorf("tạo URL upload: %w", err)
	}

	return storage.Target{
		Provider:  storage.ProviderManaged,
		URL:       u.String(),
		Method:    "PUT",
		Key:       obj,
		ExpiresAt: time.Now().Add(uploadTTL),
	}, nil
}

// Confirm xác nhận upload đã xong và ghi nhận kích thước THẬT.
//
// Đây là lớp cưỡng chế hạn mức thật sự, vì presigned PUT không ràng buộc được
// kích thước. Nếu file vượt hạn mức, xoá luôn và báo lỗi — giữ lại nghĩa là hạn
// mức chỉ là gợi ý.
func (s *Store) Confirm(ctx context.Context, userID, objectKeyFull string) (int64, error) {
	if !strings.HasPrefix(objectKeyFull, "users/"+userID+"/") {
		return 0, fmt.Errorf("%w: khoá không thuộc về người dùng này", ErrUnsafeKey)
	}

	info, err := s.client.StatObject(ctx, s.bucket, objectKeyFull, minio.StatObjectOptions{})
	if err != nil {
		return 0, fmt.Errorf("đọc thông tin đối tượng: %w", err)
	}

	limit, err := s.quota(ctx, userID)
	if err != nil {
		return 0, err
	}
	used, err := s.usage.Used(ctx, userID)
	if err != nil {
		return 0, err
	}

	if used+info.Size > limit {
		// Xoá trước rồi mới báo lỗi: nếu xoá thất bại, người dùng vẫn bị từ chối
		// và ta có rác cần dọn — thà vậy còn hơn cho qua một file vượt hạn mức.
		if err := s.client.RemoveObject(ctx, s.bucket, objectKeyFull, minio.RemoveObjectOptions{}); err != nil {
			return 0, fmt.Errorf("%w (và không xoá được đối tượng vượt hạn mức: %v)",
				storage.ErrQuotaExceeded, err)
		}
		return 0, fmt.Errorf("%w: file %d byte, chỉ còn %d byte",
			storage.ErrQuotaExceeded, info.Size, limit-used)
	}

	if err := s.usage.Add(ctx, userID, info.Size); err != nil {
		return 0, fmt.Errorf("cập nhật dung lượng đã dùng: %w", err)
	}
	return info.Size, nil
}

func (s *Store) Download(ctx context.Context, userID, key string) (storage.Target, error) {
	obj, err := objectKey(userID, strings.TrimPrefix(key, "users/"+userID))
	if err != nil {
		return storage.Target{}, err
	}
	u, err := s.client.PresignedGetObject(ctx, s.bucket, obj, downloadTTL, url.Values{})
	if err != nil {
		return storage.Target{}, fmt.Errorf("tạo URL tải về: %w", err)
	}
	// Không cần header: quyền đã được ký thẳng vào URL.
	return storage.Target{
		Provider:  storage.ProviderManaged,
		URL:       u.String(),
		Method:    "GET",
		Key:       obj,
		ExpiresAt: time.Now().Add(downloadTTL),
	}, nil
}

func (s *Store) Delete(ctx context.Context, userID, key string) error {
	obj, err := objectKey(userID, strings.TrimPrefix(key, "users/"+userID))
	if err != nil {
		return err
	}

	// Đọc kích thước TRƯỚC khi xoá để trừ vào dung lượng đã dùng. Xoá trước rồi
	// mới hỏi thì không bao giờ biết được đã giải phóng bao nhiêu, và hạn mức sẽ
	// trôi dần cho tới khi người dùng không upload được gì dù đã xoá hết ảnh.
	info, statErr := s.client.StatObject(ctx, s.bucket, obj, minio.StatObjectOptions{})

	if err := s.client.RemoveObject(ctx, s.bucket, obj, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("xoá đối tượng: %w", err)
	}
	if statErr == nil {
		if err := s.usage.Add(ctx, userID, -info.Size); err != nil {
			return fmt.Errorf("cập nhật dung lượng đã dùng: %w", err)
		}
	}
	return nil
}

func (s *Store) Usage(ctx context.Context, userID string) (storage.Usage, error) {
	used, err := s.usage.Used(ctx, userID)
	if err != nil {
		return storage.Usage{}, err
	}
	limit, err := s.quota(ctx, userID)
	if err != nil {
		return storage.Usage{}, err
	}
	return storage.Usage{
		Provider:   storage.ProviderManaged,
		UsedBytes:  used,
		LimitBytes: limit,
		Enforced:   true,
	}, nil
}

var _ storage.Provider = (*Store)(nil)
