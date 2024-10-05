package ptr

func ToPtr[T any](v T) *T {
	return &v
}
