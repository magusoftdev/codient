package tools

import "strings"

// Built-in documentation / code-reference host allowlist, derived from the same preset style as Claude Code's
// WebFetch preapproved list. Entries that are Anthropic-specific in the source are omitted:
// platform.claude.com, code.claude.com, modelcontextprotocol.io, agentskills.io, github.com/anthropics.
//
// Path-scoped source entry vercel.com/docs is enforced via preapprovedFetchPathPrefixes.

var preapprovedFetchFullHosts = []string{
	"angular.io",
	"asp.net",
	"blazor.net",
	"bun.sh",
	"cloud.google.com",
	"cypress.io",
	"d3js.org",
	"dev.mysql.com",
	"devcenter.heroku.com",
	"developer.android.com",
	"developer.apple.com",
	"developer.mozilla.org",
	"doc.rust-lang.org",
	"docs.aws.amazon.com",
	"docs.djangoproject.com",
	"docs.flutter.dev",
	"docs.netlify.com",
	"docs.oracle.com",
	"docs.python.org",
	"docs.spring.io",
	"docs.swift.org",
	"docs.unity.com",
	"docs.unrealengine.com",
	"dotnet.microsoft.com",
	"en.cppreference.com",
	"expressjs.com",
	"fastapi.tiangolo.com",
	"flask.palletsprojects.com",
	"getbootstrap.com",
	"git-scm.com",
	"go.dev",
	"gradle.org",
	"graphql.org",
	"hibernate.org",
	"httpd.apache.org",
	"huggingface.co",
	"jestjs.io",
	"jquery.com",
	"jupyter.org",
	"keras.io",
	"kubernetes.io",
	"kotlinlang.org",
	"laravel.com",
	"learn.microsoft.com",
	"maven.apache.org",
	"matplotlib.org",
	"nextjs.org",
	"nginx.org",
	"nodejs.org",
	"nuget.org",
	"numpy.org",
	"pandas.pydata.org",
	"pkg.go.dev",
	"prisma.io",
	"pytorch.org",
	"react.dev",
	"reactnative.dev",
	"reactrouter.com",
	"redis.io",
	"redux.js.org",
	"requests.readthedocs.io",
	"ruby-doc.org",
	"scikit-learn.org",
	"selenium.dev",
	"spark.apache.org",
	"symfony.com",
	"tailwindcss.com",
	"threejs.org",
	"tomcat.apache.org",
	"vuejs.org",
	"webpack.js.org",
	"wordpress.org",
	"www.ansible.com",
	"www.docker.com",
	"www.kaggle.com",
	"www.mongodb.com",
	"www.php.net",
	"www.postgresql.org",
	"www.sqlite.org",
	"www.terraform.io",
	"www.tensorflow.org",
	"www.typescriptlang.org",
}

var preapprovedFetchPathPrefixes = map[string][]string{
	"vercel.com": {"/docs"},
}

func hostMatchesPreapprovedRuleHost(host, ruleHost string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	ruleHost = strings.ToLower(strings.TrimSpace(ruleHost))
	if host == "" || ruleHost == "" {
		return false
	}
	return host == ruleHost || strings.HasSuffix(host, "."+ruleHost)
}

// pathMatchesPreapprovedPrefix enforces segment boundaries: path == p or path starts with p + "/".
func pathMatchesPreapprovedPrefix(path, prefix string) bool {
	if prefix != "" && !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	if path == "" {
		path = "/"
	}
	if path == prefix {
		return true
	}
	return strings.HasPrefix(path, prefix+"/")
}

// PreapprovedFetchAllows reports whether host+path is allowed by the built-in preset.
func PreapprovedFetchAllows(host, path string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return false
	}
	if path == "" {
		path = "/"
	}
	for ruleHost, prefs := range preapprovedFetchPathPrefixes {
		if !hostMatchesPreapprovedRuleHost(h, ruleHost) {
			continue
		}
		for _, p := range prefs {
			if pathMatchesPreapprovedPrefix(path, p) {
				return true
			}
		}
		return false
	}
	return hostAllowedFetch(h, preapprovedFetchFullHosts)
}
