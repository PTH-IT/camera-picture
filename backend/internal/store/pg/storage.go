package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hauph/camera/backend/internal/billing"
	"github.com/hauph/camera/backend/internal/secrets"
	"github.com/hauph/camera/backend/internal/storage"
	"github.com/hauph/camera/backend/internal/storage/gdrive"
)

// StorageRepo cài SelectionStore, UsageRepo và gdrive.TokenStore.
//
// Gộp ba interface vào một struct vì chúng cùng thao tác trên storage_links và
// storage_usage, và tách ra chỉ tạo thêm ba con trỏ pool giống hệt nhau.
type StorageRepo struct {
	pool   *pgxpool.Pool
	cipher *secrets.Cipher
	now    func() time.Time
}

func NewStorageRepo(pool *pgxpool.Pool, cipher *secrets.Cipher, now func() time.Time) *StorageRepo {
	if now == nil {
		now = time.Now
	}
	return &StorageRepo{pool: pool, cipher: cipher, now: now}
}

// --- SelectionStore ---

func (r *StorageRepo) Selected(userID string) (storage.ProviderID, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var p string
	err := r.pool.QueryRow(ctx, `
		SELECT provider FROM storage_links
		WHERE user_id = $1 AND revoked_at IS NULL
		ORDER BY linked_at DESC LIMIT 1`, userID).Scan(&p)
	if errors.Is(err, pgx.ErrNoRows) {
		// Mặc định là device: ảnh ở lại trên thẻ và điện thoại. Khớp với kiến trúc
		// của ADR 0001 và không tốn gì của ai.
		return storage.ProviderDevice, nil
	}
	if err != nil {
		if isInvalidUUID(err) {
			return storage.ProviderDevice, nil
		}
		return "", fmt.Errorf("đọc lựa chọn lưu trữ: %w", err)
	}
	return storage.ProviderID(p), nil
}

func (r *StorageRepo) Select(userID string, p storage.ProviderID) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := r.pool.Exec(ctx, `
		INSERT INTO storage_links (user_id, provider, linked_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, provider) DO UPDATE SET
			linked_at = EXCLUDED.linked_at,
			revoked_at = NULL`,
		userID, string(p), r.now())
	if err != nil {
		return fmt.Errorf("lưu lựa chọn: %w", err)
	}
	return nil
}

// --- UsageRepo (chỉ áp dụng cho provider managed) ---

func (r *StorageRepo) Used(ctx context.Context, userID string) (int64, error) {
	var used int64
	err := r.pool.QueryRow(ctx, `SELECT used_bytes FROM storage_usage WHERE user_id = $1`, userID).
		Scan(&used)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		if isInvalidUUID(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("đọc dung lượng đã dùng: %w", err)
	}
	return used, nil
}

// Add cộng dồn nguyên tử ngay trong câu lệnh SQL.
//
// Cố ý KHÔNG đọc-rồi-ghi từ Go: hai upload hoàn tất cùng lúc sẽ cùng đọc giá trị
// cũ và một trong hai lần cộng biến mất. Với hạn mức, mất mát đó tích luỹ theo
// một chiều và cuối cùng người dùng dùng được nhiều hơn mức đã mua.
//
// GREATEST(...,0) chặn số âm khi xoá file mà bản ghi dung lượng đã lệch.
func (r *StorageRepo) Add(ctx context.Context, userID string, delta int64) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO storage_usage (user_id, used_bytes, updated_at)
		VALUES ($1, GREATEST($2, 0), $3)
		ON CONFLICT (user_id) DO UPDATE SET
			used_bytes = GREATEST(storage_usage.used_bytes + $2, 0),
			updated_at = EXCLUDED.updated_at`,
		userID, delta, r.now())
	if err != nil {
		return fmt.Errorf("cập nhật dung lượng đã dùng: %w", err)
	}
	return nil
}

// --- gdrive.TokenStore ---

func (r *StorageRepo) RefreshToken(ctx context.Context, userID string) (string, error) {
	if r.cipher == nil {
		return "", fmt.Errorf("chưa cấu hình khoá mã hoá — không đọc được refresh token")
	}

	var enc []byte
	err := r.pool.QueryRow(ctx, `
		SELECT refresh_token_enc FROM storage_links
		WHERE user_id = $1 AND provider = 'google_drive' AND revoked_at IS NULL`, userID).
		Scan(&enc)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", gdrive.ErrNoRefreshToken
	}
	if err != nil {
		if isInvalidUUID(err) {
			return "", gdrive.ErrNoRefreshToken
		}
		return "", fmt.Errorf("đọc refresh token: %w", err)
	}
	if len(enc) == 0 {
		return "", gdrive.ErrNoRefreshToken
	}

	// Ngữ cảnh phải khớp chính xác với lúc mã hoá. Nhờ đó bản mã bị chép sang
	// dòng người dùng khác trong cơ sở dữ liệu là vô dụng — xem internal/secrets.
	plain, err := r.cipher.Decrypt(enc, secrets.LinkContext(userID, string(storage.ProviderGoogleDrive)))
	if err != nil {
		// Giải mã thất bại nghĩa là khoá đã đổi, dữ liệu bị sửa, hoặc bản mã
		// thuộc về người khác. Không có cách nào tự khắc phục — buộc liên kết lại.
		return "", fmt.Errorf("%w: giải mã thất bại", gdrive.ErrNoRefreshToken)
	}
	return string(plain), nil
}

func (r *StorageRepo) SaveRefreshToken(ctx context.Context, userID, token string) error {
	if r.cipher == nil {
		return fmt.Errorf("chưa cấu hình khoá mã hoá — từ chối lưu refresh token dạng thô")
	}
	enc, err := r.cipher.Encrypt([]byte(token), secrets.LinkContext(userID, string(storage.ProviderGoogleDrive)))
	if err != nil {
		return fmt.Errorf("mã hoá refresh token: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO storage_links (user_id, provider, refresh_token_enc, linked_at)
		VALUES ($1, 'google_drive', $2, $3)
		ON CONFLICT (user_id, provider) DO UPDATE SET
			refresh_token_enc = EXCLUDED.refresh_token_enc,
			linked_at = EXCLUDED.linked_at,
			revoked_at = NULL`,
		userID, enc, r.now())
	if err != nil {
		return fmt.Errorf("lưu refresh token: %w", err)
	}
	return nil
}

func (r *StorageRepo) RootFolderID(ctx context.Context, userID string) (string, error) {
	var id *string
	err := r.pool.QueryRow(ctx, `
		SELECT root_folder_id FROM storage_links
		WHERE user_id = $1 AND provider = 'google_drive' AND revoked_at IS NULL`, userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		if isInvalidUUID(err) {
			return "", nil
		}
		return "", err
	}
	if id == nil {
		return "", nil
	}
	return *id, nil
}

func (r *StorageRepo) SaveRootFolderID(ctx context.Context, userID, folderID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE storage_links SET root_folder_id = $2
		WHERE user_id = $1 AND provider = 'google_drive'`, userID, folderID)
	return err
}

// RevokeLink đánh dấu liên kết đã bị thu hồi.
//
// Giữ bản ghi thay vì xoá: khi người dùng hỏi "vì sao ảnh của tôi không mở được",
// bản ghi có revoked_at là câu trả lời. Xoá hẳn thì không còn gì để giải thích.
func (r *StorageRepo) RevokeLink(ctx context.Context, userID string, p storage.ProviderID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE storage_links SET revoked_at = $3, refresh_token_enc = NULL
		WHERE user_id = $1 AND provider = $2 AND revoked_at IS NULL`,
		userID, string(p), r.now())
	return err
}

// --- billing.Repo ---

type BillingRepo struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewBillingRepo(pool *pgxpool.Pool, now func() time.Time) *BillingRepo {
	if now == nil {
		now = time.Now
	}
	return &BillingRepo{pool: pool, now: now}
}

func (r *BillingRepo) ByTransaction(ctx context.Context, p billing.Platform, txID string) (billing.Entitlement, error) {
	var e billing.Entitlement
	var platform string
	err := r.pool.QueryRow(ctx, `
		SELECT platform, transaction_id, user_id, product_id, storage_bytes,
		       expires_at, revoked, updated_at
		FROM entitlements WHERE platform = $1 AND transaction_id = $2`,
		string(p), txID).
		Scan(&platform, &e.TransactionID, &e.UserID, &e.ProductID, &e.StorageBytes,
			&e.ExpiresAt, &e.Revoked, &e.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return billing.Entitlement{}, billing.ErrEntitlementNotFound
	}
	if err != nil {
		return billing.Entitlement{}, fmt.Errorf("đọc quyền lợi: %w", err)
	}
	e.Platform = billing.Platform(platform)
	return e, nil
}

// Upsert ghi quyền lợi.
//
// Khoá chính (platform, transaction_id) là thứ chặn hai lạm dụng cùng lúc: phát
// lại cùng một hoá đơn để cộng dồn dung lượng, và chia một lần mua cho nhiều tài
// khoản. user_id KHÔNG nằm trong danh sách cập nhật — một giao dịch đã thuộc về
// ai thì thuộc về người đó vĩnh viễn.
func (r *BillingRepo) Upsert(ctx context.Context, e billing.Entitlement) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO entitlements (platform, transaction_id, user_id, product_id,
			storage_bytes, expires_at, revoked, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8)
		ON CONFLICT (platform, transaction_id) DO UPDATE SET
			product_id = EXCLUDED.product_id,
			storage_bytes = EXCLUDED.storage_bytes,
			expires_at = EXCLUDED.expires_at,
			revoked = EXCLUDED.revoked,
			updated_at = EXCLUDED.updated_at`,
		string(e.Platform), e.TransactionID, e.UserID, e.ProductID,
		e.StorageBytes, e.ExpiresAt, e.Revoked, r.now())
	if err != nil {
		return fmt.Errorf("ghi quyền lợi: %w", err)
	}
	return nil
}

func (r *BillingRepo) OfUser(ctx context.Context, userID string) ([]billing.Entitlement, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT platform, transaction_id, user_id, product_id, storage_bytes,
		       expires_at, revoked, updated_at
		FROM entitlements WHERE user_id = $1`, userID)
	if err != nil {
		if isInvalidUUID(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("đọc quyền lợi: %w", err)
	}
	defer rows.Close()

	var out []billing.Entitlement
	for rows.Next() {
		var e billing.Entitlement
		var platform string
		if err := rows.Scan(&platform, &e.TransactionID, &e.UserID, &e.ProductID,
			&e.StorageBytes, &e.ExpiresAt, &e.Revoked, &e.UpdatedAt); err != nil {
			return nil, err
		}
		e.Platform = billing.Platform(platform)
		out = append(out, e)
	}
	return out, rows.Err()
}

var (
	_ billing.Repo        = (*BillingRepo)(nil)
	_ gdrive.TokenStore   = (*StorageRepo)(nil)
	_ miniostoreUsageRepo = (*StorageRepo)(nil)
)

// miniostoreUsageRepo khai lại chữ ký của miniostore.UsageRepo để khẳng định kiểu
// tại đây mà không phải import ngược package đó — tránh vòng phụ thuộc.
type miniostoreUsageRepo interface {
	Used(ctx context.Context, userID string) (int64, error)
	Add(ctx context.Context, userID string, delta int64) error
}
