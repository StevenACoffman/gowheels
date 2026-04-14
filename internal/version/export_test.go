package version

// ResolveEnvVersion exposes resolveEnvVersion for testing without modifying
// os.Getenv. getenv receives the environment variable name and returns the value.
func ResolveEnvVersion(getenv func(string) string) (string, bool) {
	return resolveEnvVersion(getenv)
}
