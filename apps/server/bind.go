package main

const (
	bindLoopback      = "127.0.0.1"
	bindAllInterfaces = "0.0.0.0"
)

// resolveBind picks the default bind address when the operator did not pass
// -bind explicitly. Locally we want 127.0.0.1 so the API isn't reachable to
// other users on the host; PaaS / container deploys flip to 0.0.0.0 because
// the platform's router is on a different interface. prod is true when
// STACKIT_ENV=production, which signals a hosted runtime.
func resolveBind(flagBind *string, bindExplicit, prod bool) {
	if bindExplicit {
		return
	}
	if prod {
		*flagBind = bindAllInterfaces
		return
	}
	*flagBind = bindLoopback
}
