// This is a separate module so the alcatraz core stays dependency-free: the
// backtracking regexp2 engine is only pulled in if you import this package.
module github.com/hoophq/alcatraz/lookaround

go 1.24

require github.com/hoophq/alcatraz v0.0.0

require github.com/dlclark/regexp2 v1.12.0

// Local development: resolve the parent module from the repo, not the proxy.
replace github.com/hoophq/alcatraz => ../
