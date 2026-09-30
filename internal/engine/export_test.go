package engine

// Impl exposes the concrete engine to the external engine_test package so
// tests can drive implementation methods that are deliberately kept off the
// Engine interface.
func Impl(e Engine) *engineImpl { //nolint:revive // test-only accessor to the concrete type
	return e.(*engineImpl)
}
