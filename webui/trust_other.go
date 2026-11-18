//go:build !unix

package webui

// checkTrusted: ownership and mode bits mean something else here (ACLs), and
// this build has not been exercised outside unix; it trusts the path it was
// given. Serve a UI you built yourself with --web DIR if that is not enough.
func checkTrusted(root, modcache string) error { return nil }
