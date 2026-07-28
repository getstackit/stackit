package utils

// OpenBrowser opens a URL in the user's default browser. It is a package
// variable (rather than a plain function) so tests can override it instead
// of shelling out to a real browser launcher.
var OpenBrowser = openBrowser
