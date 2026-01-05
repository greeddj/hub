# HUB - Caching proxy server

Config example:

```yaml
dir: _data
server:
  pypi:
    pypi.org: https://pypi.org/simple
  rubygems:
    rubygems: https://rubygems.org
  galaxy:
    ansible:
      url: https://galaxy.ansible.com
    test:
      dir: _galaxy
  static:
    github: https://github.com
    k8s: https://dl.k8s.io
    get_helm: https://get.helm.sh
  goproxy:
    golang: https://proxy.golang.org
  npm:
    npmjs: https://registry.npmjs.org
  cargo:
    my-registry: https://registry.example.com
    crates.io:
      base: https://crates.io
      index: https://index.crates.io
```

## Usage

### PyPI

Access cached PyPI packages:

```text
http://localhost:6587/pypi/pypi.org/simple/{package}/
```

### Ansible Galaxy

Access cached Galaxy collections:

```text
http://localhost:6587/galaxy/ansible/api/v3/collections/{namespace}/{name}/
```

### RubyGems

Use HUB as a RubyGems/Bundler source:

```text
http://localhost:6587/rubygems/{repo_name}
```

Bundler/RubyGems clients fetch a set of plain HTTP resources. HUB proxies and caches any path under the configured upstream (wildcard route), including common endpoints:

- Compact index: `/names`, `/versions`, `/info/<gem>`
- Legacy indexes: `/specs.4.8.gz`, `/latest_specs.4.8.gz`, `/prerelease_specs.4.8.gz`, `/quick/Marshal.4.8/*.gemspec.rz`
- Gem artifacts: `/gems/<name>-<version>.gem`

Bundler mirror example:

```bash
# all requests to rubygems.org must be forwarded to localhost:6587/rubygems/rubygems
bundle config --local mirror.https://rubygems.org http://localhost:6587/rubygems/rubygems
```

Gemfile source example:

```Gemfile
# by default all requests will be forwarded to proxy
source "http://localhost:6587/rubygems/rubygems"
gem "puma"

# but for rack gem it will use some other upstream
source "https://rubygems.org" do
  gem "rack"
end
```

### Static files

Access cached static files:

```text
http://localhost:6587/static/{key}/get/{path}
```

### GOPROXY

To use HUB as a Go module proxy, set the `GOPROXY` environment variable:

```bash
export GOPROXY=http://localhost:6587/goproxy/golang
```

Or configure it for a specific project:

```bash
go env -w GOPROXY=http://localhost:6587/goproxy/golang
```

The proxy supports all standard GOPROXY protocol endpoints:

- `/{module}/@v/list` - list of available versions
- `/{module}/@v/{version}.info` - version metadata (JSON)
- `/{module}/@v/{version}.mod` - go.mod file for the version
- `/{module}/@v/{version}.zip` - source code archive
- `/{module}/@latest` - latest version info

### NPM

To use HUB as an npm registry proxy, set the `registry` to your HUB instance:

```bash
npm config set registry http://localhost:6587/npm/npmjs
```

For a scoped registry:

```bash
npm config set @my-scope:registry http://localhost:6587/npm/npmjs
```

Or it can be used in the file `.npmrc`

```ini
registry=http://localhost:6587/npm/npmjs
```

The proxy supports the following npm registry endpoints:

- `/{package}` - package metadata (packument)
- `/@scope%2F{name}` - scoped package metadata (packument)
- `/{package}/-/{tarball}.tgz` - package tarball
- `/@scope/{name}/-/{tarball}.tgz` - scoped package tarball
- `/-/v1/search` - search (cached for 10 minutes)

### Cargo (Rust registry)

To use HUB as a Cargo registry proxy, add a registry to `.cargo/config.toml`:

```toml
[registries]
hub = { index = "sparse+http://localhost:6587/cargo/crates.io/" }
```

You can also use an env var instead of a config file:

```bash
export CARGO_REGISTRIES_HUB_INDEX=sparse+http://localhost:6587/cargo/crates.io/
```

For full replace crates.io:

```toml
[source.crates-io]
replace-with = "hub"

[source.hub]
registry = "sparse+http://localhost:6587/cargo/crates.io/"
```

The proxy supports `sparse` index files, crate downloads, and `cargo search` via `GET`/`HEAD` requests.

Cargo proxy fields:

- `base` (required) — registry base URL.
- `index` (optional) — index URL; defaults to `base + /index`.
- `dl` (optional) — crate download URL; defaults to `base + /api/v1/crates`.
- `api` (optional) — API URL; defaults to `base + /api`.

The proxy supports the following Cargo endpoints:

- `/cargo/<key>/index/config.json` — generated `config.json`.
- `/cargo/<key>/index/*` — sparse index proxy/cache.
- `/cargo/<key>/config.json` — generated `config.json` (index root alias).
- `/cargo/<key>/*` — sparse index proxy/cache (index root).
- `/cargo/<key>/crates/{crate}/{version}/download` — crate download proxy/cache.
- `/cargo/<key>/api/*` — API proxy (for `cargo search`).

For registries with a dedicated index host (like `crates.io`), set `index` explicitly.

Cargo proxy limitations:

- Only the sparse index protocol is supported (no git index).
- Authorization headers are not forwarded (no private registries).
