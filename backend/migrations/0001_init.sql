-- Lược đồ khởi tạo.
--
-- Ba quyết định định hình toàn bộ file này, đều bắt nguồn từ
-- docs/adr/0001-capture-strategy.md:
--
--   1. PHẦN LỚN ẢNH KHÔNG BAO GIỜ LÊN SERVER. Một đám cưới là 2000 shot,
--      hơn 100GB NEF, nằm trên thẻ nhớ. Server chỉ giữ metadata cho tất cả,
--      và file thật cho số ít ảnh được chọn. Vì vậy `images` tồn tại độc lập
--      với `image_assets`, và một ảnh không có asset nào là chuyện BÌNH THƯỜNG.
--
--   2. CHỈNH SỬA KHÔNG PHÁ HUỶ. `image_edits` tách khỏi `images` để người dùng
--      luôn quay lại được ảnh gốc và so sánh được nhiều preset trên cùng một ảnh.
--
--   3. ĐỒNG BỘ BẰNG SỐ HIỆU LOGIC, KHÔNG BẰNG ĐỒNG HỒ. Xem `sessions.revision`.

CREATE TABLE users (
    id          uuid PRIMARY KEY,
    email       text NOT NULL UNIQUE,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Một buổi chụp.
CREATE TABLE sessions (
    id          uuid PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        text NOT NULL,
    client_name text,
    started_at  timestamptz NOT NULL,
    ended_at    timestamptz,

    -- Đồng hồ logic của buổi chụp. Tăng 1 mỗi khi có thay đổi bất kỳ thuộc
    -- buổi này. Client đồng bộ delta bằng "cho tôi mọi thứ có revision > N".
    --
    -- Cố ý KHÔNG dùng timestamp làm con trỏ đồng bộ: đồng hồ máy ảnh, đồng hồ
    -- điện thoại và đồng hồ server đều lệch nhau, và hai thay đổi trong cùng
    -- một mili giây sẽ khiến client bỏ sót bản ghi. Số nguyên tăng dần do
    -- server cấp phát không có cả hai vấn đề đó.
    revision    bigint NOT NULL DEFAULT 0,

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ON sessions (user_id, started_at DESC);

-- Máy ảnh đã ghép nối. Lưu capabilities để biết ảnh này đến từ nguồn nào và
-- tin được tới đâu — ảnh từ Android/libgphoto2 (phase sau) sẽ có tập khả năng
-- khác hẳn ảnh từ iOS/CascableCore.
CREATE TABLE cameras (
    id            uuid PRIMARY KEY,
    user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    manufacturer  text NOT NULL,
    model         text NOT NULL,
    firmware      text,
    transport     text NOT NULL CHECK (transport IN ('usb', 'wifi')),
    capabilities  text[] NOT NULL DEFAULT '{}',
    last_seen_at  timestamptz
);

CREATE TABLE images (
    id          uuid PRIMARY KEY,
    session_id  uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,

    -- Định danh do client cấp, bắt nguồn từ item id trên thẻ nhớ.
    -- UNIQUE cùng session_id là thứ khiến việc đẩy metadata trở nên idempotent:
    -- client gửi lại cùng một lô sau khi mất mạng cũng không tạo bản trùng.
    client_id   text NOT NULL,

    filename    text NOT NULL,
    format      text NOT NULL,
    byte_size   bigint NOT NULL,

    -- Giờ của MÁY ẢNH. Có thể lệch giờ điện thoại và giờ server. Dùng để sắp
    -- xếp trong cùng một máy thì được; đừng dùng để sắp xếp tuyệt đối giữa
    -- nhiều thiết bị mà không hiệu chỉnh trước.
    captured_at timestamptz NOT NULL,

    is_raw      boolean NOT NULL,
    camera_id   uuid REFERENCES cameras(id) ON DELETE SET NULL,
    exif        jsonb NOT NULL DEFAULT '{}',

    revision    bigint NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz,

    UNIQUE (session_id, client_id)
);

-- Chỉ mục chính cho đồng bộ delta: "mọi ảnh của buổi này có revision > N".
CREATE INDEX ON images (session_id, revision);
CREATE INDEX ON images (session_id, captured_at);

-- Các phiên bản của một ảnh. Xem .claude/skills/photo-tether-app/references/backend-go.md.
--
-- Một ảnh KHÔNG có dòng nào ở đây là trạng thái bình thường — nghĩa là nó vẫn
-- nằm trên thẻ nhớ và chưa ai chọn nó.
CREATE TABLE image_assets (
    image_id    uuid NOT NULL REFERENCES images(id) ON DELETE CASCADE,
    tier        text NOT NULL CHECK (tier IN ('thumb', 'preview', 'proxy', 'original', 'export')),
    storage_key text NOT NULL,
    byte_size   bigint NOT NULL,
    width       int,
    height      int,
    created_at  timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (image_id, tier)
);

-- Preset của người dùng.
--
-- `version` nằm trong chính JSON và cũng được nhân bản ra cột để query được.
-- Preset là tài sản người dùng giữ nhiều năm; mọi thay đổi cấu trúc sau này
-- phải migrate được, và không được làm hỏng file cũ.
CREATE TABLE presets (
    id          uuid PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        text NOT NULL,
    version     int NOT NULL,
    body        jsonb NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz
);

CREATE INDEX ON presets (user_id) WHERE deleted_at IS NULL;

-- Chỉnh sửa, tách khỏi `images` để không phá huỷ ảnh gốc.
CREATE TABLE image_edits (
    image_id    uuid PRIMARY KEY REFERENCES images(id) ON DELETE CASCADE,
    preset_id   uuid REFERENCES presets(id) ON DELETE SET NULL,

    -- Điều chỉnh riêng cho ảnh này, đè lên preset.
    overrides   jsonb NOT NULL DEFAULT '{}',

    -- Cờ culling. Tách riêng khỏi overrides vì được query và lọc thường xuyên.
    rating      smallint NOT NULL DEFAULT 0 CHECK (rating BETWEEN 0 AND 5),
    flagged     boolean NOT NULL DEFAULT false,
    rejected    boolean NOT NULL DEFAULT false,

    revision    bigint NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),

    -- Thiết bị ghi lần cuối. Giải quyết xung đột hiện là last-write-wins; ghi
    -- lại nguồn ghi để sau này có thể chẩn đoán khi người dùng báo "mất chỉnh sửa".
    updated_by_device text
);

CREATE INDEX ON image_edits (revision);

CREATE TABLE jobs (
    id           uuid PRIMARY KEY,
    kind         text NOT NULL,
    status       text NOT NULL CHECK (status IN ('queued', 'running', 'done', 'failed')),
    session_id   uuid REFERENCES sessions(id) ON DELETE CASCADE,
    image_id     uuid REFERENCES images(id) ON DELETE CASCADE,
    payload      jsonb NOT NULL DEFAULT '{}',
    result       jsonb,
    error        text,
    attempts     int NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ON jobs (status, kind) WHERE status IN ('queued', 'running');

CREATE TABLE ai_results (
    id          uuid PRIMARY KEY,
    image_id    uuid NOT NULL REFERENCES images(id) ON DELETE CASCADE,
    kind        text NOT NULL,
    storage_key text,
    params      jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now(),

    UNIQUE (image_id, kind)
);
