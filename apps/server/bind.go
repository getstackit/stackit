package main

// resolveBind picks the default bind address when the operator did not pass
// -bind explicitly. Locally we want 127.0.0.1 so the API isn't reachable to
// other users on the host; PaaS / container deploys flip to 0.0.0.0 because
// the platform's router is on a different interface. publicHint is true when
// $PORT or $STACKIT_PUBLIC are set, both of which signal a hosted runtime.
func resolveBind(flagBind *string, bindExplicit, publicHint bool) {
	if bindExplicit {
		return
	}
	if publicHint {
		*flagBind = "0.0.0.0"
		return
	}
	*flagBind = "127.0.0.1"
}
