package updatecheck

// NewFetcherForTest returns a Fetcher that downloads from base (a local test
// server, plain HTTP allowed) and accepts redirects only to the given hosts.
// Real code uses NewFetcher, which is fixed to GitHub.
func NewFetcherForTest(base string, hosts ...string) *Fetcher {
	f := &Fetcher{Repo: DefaultRepo, Base: base, Version: "test"}
	f.setHosts(hosts, true)
	return f
}
