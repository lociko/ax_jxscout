# AX JXScout

This repository is a GPL-3.0 fork of
[francisconeves97/jxscout](https://github.com/francisconeves97/jxscout). It adds
`ax-jxscout`, a non-interactive command for Unix pipelines and distributed
workers. The original proxy/TUI command remains available under
`cmd/jxscout`.

`ax-jxscout` accepts HTTP(S) seed URLs from stdin, an input file, or positional
arguments. It fetches each page, extracts script and preload URLs, runs the
inherited Webpack/Vite chunk detector, recursively fetches discovered chunks,
and writes newline-delimited JSON to stdout. Operational failures are records
on stdout so one unavailable target does not discard successful assets.

## Pipe command

Requirements:

- Go 1.23.2 or newer.
- Bun 1.2.12 or newer. The inherited chunk engine is embedded in the Go binary
  as JavaScript and executed with Bun; a future container image must include
  both the Go binary and Bun.

Build and test:

```bash
make build-pipe
make test-pipe
```

Read URLs from stdin:

```bash
printf 'https://example.com/\n' |
  ./dist/ax-jxscout \
    -rate-limit 2 \
    -max-assets-per-seed 500 \
  > assets.jsonl
```

Read from a file:

```bash
./dist/ax-jxscout -input-file urls.txt > assets.jsonl
```

The default scope is the host from the seed URL and its redirect destination.
Use `-allow-cross-origin` only when referenced CDN hosts are authorized. Run
`./dist/ax-jxscout -h` for all limits and concurrency options.

Each `asset` record contains the effective URL, parent relationship, response
metadata, SHA-256, and the exact response body encoded as `gzip+base64`.
`error` records describe target-level failures. One `summary` record per seed
has a status of `complete`, `complete_with_errors`, `failed`, or `truncated`.

The command discovers JavaScript statically reachable from the supplied pages.
It does not drive a browser, crawl application routes, authenticate, or promise
coverage of URLs assembled only at runtime.

## Upstream project

> ⚠️ This version of JXScout is no longer actively maintained.
>
> JXScout has evolved a lot since this open source version was first released. The project has been completely rewritten from scratch in Rust as JXScout Pro, which is a separate, closed source codebase. JXScout Pro is a completely different product — it doesn't share any code with this version and is far more capable.
>
> You're welcome to experiment with this version, but be aware it has known bugs that will impact your coverage. For the full experience, check out JXScout Pro.
>
> I am always happy to offer free trials for the Pro version! You can get it through https://jxscout.app/
>
> Please join the discord server to follow updates or for any questions: https://discord.com/invite/qu9AMdjZFj

# jxscout

**jxscout** is a tool designed to help security researchers analyze and find vulnerabilities in JavaScript code. It works with your favorite proxy (Burp or Caido), capturing requests and saving optimized versions locally for easy analysis in your preferred code editor.

## Key Features

- **Asset Organization**: Automatically saves and organizes relevant static assets (HTML, JavaScript) into an intuitive folder structure.
- **Chunks Pre-Fetching**: Detects and pre-fetches Webpack and Vite chunks for comprehensive analysis.
- **Code Beautification**: Automatically beautifies JavaScript files, making them easier to read and analyze.
- **Source Map Discovery**: Automatically reverses application source code if .map files are available.
- **AST Analysis**: Automatically analyzes JavaScript files finding interesting functionality for vulnerability researchers. See [VSCode Extension](https://github.com/francisconeves97/jxscout-vscode) to learn how to install the extension.

## Demo

Watch the demo movie to see jxscout in action:

https://github.com/user-attachments/assets/64f161c3-46b0-41a9-8b34-706cc795a034

## Installation & Instructions

### Requirements

- **golang**: https://go.dev/doc/install - jxscout is written in golang
- **bun** (>=1.2.12 recommended): https://bun.sh/docs/installation - used for the chunk discovery script

### Part 1. Installing the CLI

The first step to get up and running with jxscout is to install the CLI. To do that, just run the command below or download a pre-compiled binary from the [release page](https://github.com/francisconeves97/jxscout/releases).

```
go install github.com/francisconeves97/jxscout/cmd/jxscout@latest
```

![jxscout](docs/jxscout.png)

The jxscout CLI has a prompt with many commands, one of them is `install` which installs `bun` and `prettier`, which are needed dependencies of jxscout. If you don't have those tools installed, feel free to install them manually or by running the `install` command.

### Part 2. Proxy Setup

jxscout will only start capturing assets after you setup your proxy to forward requests to it. To do that, you should install the right plugin for your proxy:

- For Caido users: Check out https://github.com/francisconeves97/jxscout-caido
- For Burp users: Check out https://github.com/francisconeves97/jxscout-burp

After you install the proxy plugin, every asset that flows through your proxy should be automatically saved to your `~/jxscout` folder.

One useful feature is the ability to organize your assets in projects (similar to how you would do in your proxy). To do this, you can run `config project-name=my_project` in the jxscout CLI, and from that moment on, assets will be saved in `~/jxscout/my_project`.

If you don't see assets being saved at this point, you should check the [Troubleshooting](#troubleshooting) section.

### Part 3. VSCode Extension

The VSCode extension is a frontend for jxscout. It allows you to visualize the AST Analysis results of jxscout in a useful tree. It also allows you to copy values from there, including paths and query params for bruteforcing.

![jxscout](docs/vscode.png)

To get started with the VSCode Extension head over to https://github.com/francisconeves97/jxscout-vscode for more instructions.

You can check the structure of the analysis tree on [docs/tree.md](./docs/tree.md) to get a picture of what descriptors you will get.

## Troubleshooting

This section should help you troubleshoot problems with jxscout.

### Some files are not being downloaded

jxscout will only save assets that go through your proxy. This means that if the browser is caching some of those assets, jxscout will have no visibility of them. Because of this, it's important to disable your browser's cache when using jxscout.

If you still don't see some assets, they could be out of scope and filtered, or it could be a bug.

### View logs

jxscout provides a log view, which you can access by typing `l` in the CLI prompt. However, you can also access the log file directly by opening `~/.jxscout/jxscout.log`.

You can turn on debug logging by using `config debug=true`, which will give you more verbose output from jxscout.

### If you don't see any relevant logs

Your proxy plugin might not be feeding requests into jxscout. Check your jxscout proxy plugin settings and verify that:

- It is enabled and working
- The configured hostname/port matches where jxscout is running
- You are trying to access an in-scope website if the "Filter requests in-scope" option is enabled on the proxy plugin

### Understanding jxscout internals

jxscout uses SQLite and bundles some JS scripts that are run by bun. All of these files will be extracted to your `~/.jxscout/` directory.

### Opening up a github issue

If you couldn't figure out the issue or if you found a bug, feel free to open an issue as I am always happy to help!

## Usage

```bash
jxscout
```

Once jxscout is running, you can:

- Run the `guide` command for a quick walkthrough
- Run the `help` command for an overview of the available commands
- Watch the video tutorial for a visual guide on configuring and using jxscout

### Available Commands

These are the commands available on the `jxscout` prompt. You can run `help` to get this list.

```
Available commands:

assets (la) - List assets for the current project with pagination and search
  Usage: assets [page=<page_number>] [page-size=<page_size>] [search=<search_term>]

caido-auth (ca) - Authenticate with Caido to use overrides (token is stored in memory and will reset on server restart)
  Usage: caido-auth

clear (c) - Clears the output
  Usage: clear

config (cf) - View or update jxscout configuration options
  Usage: config [options] | Use 'config' without arguments to view current configuration

config-reset (cfr) - Reset all configuration options to default values
  Usage: config-reset

exit (q) - Exits the application
  Usage: exit

guide (g) - Show a guide on how to use jxscout
  Usage: guide

help (h) - Shows help information for commands
  Usage: help [command]

install (i) - Install jxscout dependencies (npm, bun, prettier)
  Usage: install

loaded (ldd) - Show assets that loaded a specific JavaScript asset
  Usage: loaded <asset_url> [page=<page_number>] [page-size=<page_size>]

loads (lds) - Show JavaScript assets loaded by a specific HTML page
  Usage: loads <html_url> [page=<page_number>] [page-size=<page_size>]

logs (l) - Toggle logs panel
  Usage: logs

override (o) - Toggle local override for a specific URL (only available for Caido).
This will override the content of an asset when you visit it in your browser.
When overriding an HTML file keep the (index).html suffix.
The `assets` command will give you the right URL to use.
  Usage: override <url>

overrides (lo) - List overrides
  Usage: overrides [page=<page_number>] [page-size=<page_size>]

truncate-tables (tt) - Delete all data tracked in jxscout database (requires confirmation)
  Usage: truncate-tables

version (v) - Show the current version and checks for updates
  Usage: version
```

### CLI Options

You can configure jxscout using the `config` prompt command, but if you can also pass these flags directly to jxscout.

```bash
jxscout | static files downloader for vulnerability analysis

Usage:
  jxscout [flags]

Flags:
SERVER CONFIGURATION:
   -hostname string  the hostname where jxscout will listen for requests (default "localhost")
   -port int         the port where jxscout will listen for requests (default 3333)

JXSCOUT CONFIGURATION:
   -project-name string  name of your project folder where downloaded files will be stored (default "default")
   -scope string[]       comma-separated list of patterns to filter requests (e.g. *google*,*youtube*)
   -debug                turn on detailed logs for troubleshooting

CONCURRENCY CONFIGURATION:
   -fetch-concurrency int             how many files to download at once (for chunks and source maps) (default 5)
   -save-concurrency int              how many files to save to disk at once (default 5)
   -beautifier-concurrency int        how many files to beautify at once (default 5)
   -chunk-discoverer-concurrency int  how many chunk discovery processes to run at once (default 5)
   -ast-analyzer-concurrency int      how many AST analysis processes to run at once (default 5)

CHUNK DISCOVERY CONFIGURATION:
   -chunk-discoverer-bruteforce-limit int  how many potential chunks to bruteforce when automatic discovery fails (default 3000)

RATE LIMITING CONFIGURATION:
   -rate-limiter-max-requests-per-second int  max requests per second for source maps and chunk discovery (0 = unlimited) (default 2)
   -rate-limiter-max-requests-per-minute int  max requests per minute for source maps and chunk discovery (0 = unlimited)

JS INGESTION CONFIGURATION:
   -download-refered-js  download JS files from out-of-scope domains if they're linked from in-scope pages

LOGGING CONFIGURATION:
   -log-buffer-size int       how many log lines to show in the logs panel (default 10000)
   -log-file-max-size-mb int  max size of the log file in MB (default 10)

OVERRIDES CONFIGURATION:
   -caido-hostname string                  hostname where Caido is running (default "localhost")
   -caido-port int                         port where Caido is running (default 8080)
   -override-content-check-interval value  interval at which to check for changes in override content and update match/replace rules (default 5s)
```

## Contributing

Feel free to leave suggestions and open pull requests, all contributions are welcome!

### Contributors ✨

Thanks goes to these wonderful people for contributing!

- [@BigCod3](https://github.com/BigCod3) 💻 🐛
- [@abdilahrf](https://github.com/abdilahrf) 💻 💡
- [@Hi-Timofey](https://github.com/Hi-Timofey) 💻

## Support

Happy hunting! 🐛 If jxscout helped you find cool bugs, [consider buying me a coffee](https://ko-fi.com/francisconeves97)! ☕

## License

This project is licensed under the GNU General Public License. See the COPYING file for the full license text.

## ⚠️ Disclaimer

This tool is intended for educational and authorized security testing purposes only.

You may use this software only on systems you own or have explicit permission to test.

Any misuse of this tool is strictly prohibited. The author is not responsible for any damage, legal issues, or misuse caused by this software.
