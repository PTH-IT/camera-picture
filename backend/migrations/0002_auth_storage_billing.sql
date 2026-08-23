-- Xác thực, lựa chọn lưu trữ, và quyền lợi dung lượng.
--
-- Xem docs/adr/0002-auth-and-storage.md về các ràng buộc bên ngoài định hình
-- lược đồ này (App Store guideline 4.8, hoa hồng IAP, scope drive.file).

-- 0001 tạo bảng users với cột email NOT NULL UNIQUE. Cả hai ràng buộc đều sai
-- với thực tế: người dùng Sign in with Apple có thể ẩn email HOÀN TOÀN, và khi
-- đó ta không có địa chỉ nào để lưu. Ép NOT NULL nghĩa là từ chối một nhóm người
-- dùng mà chính Apple bắt ta phải hỗ trợ.
ALTER TABLE users ALTER COLUMN email DROP NOT NULL;
ALTER TABLE users ADD COLUMN email_verified boolean NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN name text;

-- UNIQUE cũ không xử lý được nhiều NULL trong Postgres theo cách ta cần cho
-- xoá mềm sau này; thay bằng chỉ mục một phần, chỉ áp dụng khi có email.
ALTER TABLE users DROP CONSTRAINT users_email_key;
CREATE UNIQUE INDEX users_email_unique ON users (email) WHERE email IS NOT NULL;

-- Một người, nhiều cách đăng nhập.
--
-- Tách bảng thay vì nhét cột `provider` vào users: cùng một người hoàn toàn có
-- thể đăng nhập bằng Apple hôm nay, Google ngày mai, và mật khẩu khi đổi máy.
-- Nhét vào users là buộc họ phải có ba tài khoản.
CREATE TABLE identities (
    provider   text NOT NULL CHECK (provider IN ('apple', 'google', 'password')),

    -- subject là khoá định danh ỔN ĐỊNH từ nhà cung cấp (claim `sub`).
    --
    -- KHÔNG dùng email làm khoá. Apple cấp email relay và người dùng tắt chuyển
    -- tiếp được; Google cho đổi email chính. Lấy email làm khoá là cách chắc chắn
    -- để một ngày nào đó khoá người dùng ra khỏi tài khoản của chính họ.
    subject    text NOT NULL,

    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email      text,
    created_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (provider, subject)
);

CREATE INDEX ON identities (user_id);

CREATE TABLE user_passwords (
    user_id       uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    -- bcrypt cost 12. Không lưu mật khẩu thô, hiển nhiên; nhưng cũng không lưu
    -- hash không có salt — bcrypt đã gộp salt vào chuỗi kết quả.
    password_hash bytea NOT NULL,
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- Phiên đăng nhập dạng mờ.
--
-- Chỉ lưu HASH của token. Nếu cơ sở dữ liệu bị lộ, kẻ tấn công không mạo danh
-- được phiên nào — cùng lý do người ta không lưu mật khẩu dạng thô.
--
-- Chọn token mờ thay vì JWT tự ký để "đăng xuất khỏi mọi thiết bị" có tác dụng
-- thật sau khi người dùng mất máy. JWT không thu hồi được trước khi hết hạn.
CREATE TABLE sessions_auth (
    token_hash bytea PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- Ghi lại để hiển thị danh sách thiết bị đang đăng nhập.
    user_agent text
);

CREATE INDEX ON sessions_auth (user_id);
CREATE INDEX ON sessions_auth (expires_at);

-- Lựa chọn nơi lưu trữ của người dùng.
CREATE TABLE storage_links (
    user_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider  text NOT NULL CHECK (provider IN ('device', 'managed', 'google_drive', 'icloud')),

    -- refresh_token_enc chỉ có với google_drive, và CHỈ khi người dùng đồng ý cho
    -- server kết xuất RAW chất lượng cao — server phải đọc được file mới làm được.
    -- Phải mã hoá at-rest; cột này không bao giờ được ghi ra log.
    refresh_token_enc bytea,

    -- root_folder_id với Drive. Với scope drive.file, app chỉ thấy file DO CHÍNH
    -- NÓ tạo. Đây là ràng buộc kiến trúc cứng: đổi sang scope rộng hơn kéo theo
    -- kiểm định CASA tốn tiền và phải làm lại mỗi 12 tháng. Xem ADR 0002.
    root_folder_id text,

    linked_at  timestamptz NOT NULL DEFAULT now(),
    -- revoked_at khác NULL nghĩa là người dùng đã thu hồi quyền ở phía Google/Apple.
    -- Giữ lại bản ghi thay vì xoá để còn giải thích được vì sao ảnh không mở được.
    revoked_at timestamptz,

    PRIMARY KEY (user_id, provider)
);

-- Provider của từng asset.
--
-- Cùng một ảnh có thể có preview ở managed và bản gốc ở google_drive, nên
-- provider thuộc về ASSET chứ không thuộc về người dùng hay về ảnh.
ALTER TABLE image_assets ADD COLUMN provider text NOT NULL DEFAULT 'managed'
    CHECK (provider IN ('device', 'managed', 'google_drive', 'icloud'));

-- Quyền lợi dung lượng mua qua IAP.
CREATE TABLE entitlements (
    platform       text NOT NULL CHECK (platform IN ('apple', 'google')),

    -- transaction_id phải ỔN ĐỊNH qua các lần gia hạn: Apple dùng
    -- originalTransactionId, Google dùng purchaseToken. Lấy id của từng lần gia
    -- hạn sẽ khiến mỗi tháng sinh ra một quyền lợi mới thay vì cập nhật cái cũ,
    -- và dung lượng của người dùng sẽ tăng vô hạn theo thời gian.
    --
    -- Khoá chính (platform, transaction_id) đồng thời là thứ chặn hai lạm dụng:
    -- phát lại cùng một hoá đơn, và chia một lần mua cho nhiều tài khoản.
    transaction_id text NOT NULL,

    user_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    product_id     text NOT NULL,
    storage_bytes  bigint NOT NULL,
    expires_at     timestamptz NOT NULL,
    -- revoked = hoàn tiền hoặc huỷ. Cập nhật qua server notification của store,
    -- không phải lúc người dùng mở app: người hoàn tiền rồi không mở app nữa sẽ
    -- giữ dung lượng vĩnh viễn.
    revoked        boolean NOT NULL DEFAULT false,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (platform, transaction_id)
);

CREATE INDEX ON entitlements (user_id) WHERE NOT revoked;

-- Nhật ký dung lượng đã dùng, chỉ áp dụng cho provider 'managed'.
--
-- Với google_drive và icloud, hạn mức là chuyện giữa người dùng và Google/Apple;
-- app chỉ đọc và hiển thị chứ không cưỡng chế.
CREATE TABLE storage_usage (
    user_id     uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    used_bytes  bigint NOT NULL DEFAULT 0,
    updated_at  timestamptz NOT NULL DEFAULT now()
);
