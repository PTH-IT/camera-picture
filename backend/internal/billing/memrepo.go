package billing

import (
	"context"
	"sync"
)

// MemRepo là bản Repo trong bộ nhớ cho test và chạy thử.
//
// KHÔNG dùng cho production. Nó tồn tại để các quy tắc chống lạm dụng — chống
// phát lại hoá đơn, chống chia sẻ một lần mua cho nhiều tài khoản — được test kỹ
// mà không cần Postgres.
type MemRepo struct {
	mu sync.Mutex
	// byTx khoá theo platform + transactionID. Chính khoá này là thứ chặn việc
	// một giao dịch được dùng cho nhiều tài khoản.
	byTx map[string]Entitlement
}

func NewMemRepo() *MemRepo {
	return &MemRepo{byTx: map[string]Entitlement{}}
}

func txKey(p Platform, id string) string { return string(p) + "\x00" + id }

func (r *MemRepo) ByTransaction(_ context.Context, p Platform, txID string) (Entitlement, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	e, ok := r.byTx[txKey(p, txID)]
	if !ok {
		return Entitlement{}, ErrEntitlementNotFound
	}
	return e, nil
}

func (r *MemRepo) Upsert(_ context.Context, e Entitlement) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.byTx[txKey(e.Platform, e.TransactionID)] = e
	return nil
}

func (r *MemRepo) OfUser(_ context.Context, userID string) ([]Entitlement, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []Entitlement
	for _, e := range r.byTx {
		if e.UserID == userID {
			out = append(out, e)
		}
	}
	return out, nil
}

var _ Repo = (*MemRepo)(nil)
