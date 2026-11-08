package account

import "context"

type Data interface {
	Export(ctx context.Context, uid string) (map[string][]map[string]any, error)
	Erase(ctx context.Context, uid string) error
}
type Identity interface {
	Delete(ctx context.Context, uid string) error
}
type Calendar interface {
	Cleanup(ctx context.Context, uid string) error
}

type Usage interface {
	Forget(context.Context, string) error
}
