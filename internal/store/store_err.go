package store

type storeErr string

func (e storeErr) Error() string {
	return string(e)
}

const (
	ErrNotFound = storeErr("not found")
)
