[![Go Report Card](https://goreportcard.com/badge/github.com/Luzifer/webcheck)](https://goreportcard.com/report/github.com/Luzifer/webcheck)
![](https://badges.fyi/github/license/Luzifer/webcheck)
![](https://badges.fyi/github/downloads/Luzifer/webcheck)
![](https://badges.fyi/github/latest-release/Luzifer/webcheck)

# Luzifer / webcheck

`webcheck` is a CLI tool to check the health of a web page. It was written because there are quite a lot of GUI tools or services to monitor websites but I did not find any tool to execute on the CLI to to an ad-hoc monitoring of a page. To fix this I wrote this tool.

`webcheck` is used to check:

- HTTP status code 2xx
- Regular expression match on the response body
- Response within a certain timeout
- Response times (only display, no checking)

If the request is marked as a `FAIL` all headers and the response body are written into a log file to be debugged manually. This can for example be helpful if your webserver delivers weird responses on certain requests and you are using `webcheck` to find them.

## Installation

You either can download a pre-compiled binary for you system from the [Releases](https://github.com/Luzifer/webcheck/releases) section of this repository or if you do have a working Go environment you can just `go get github.com/Luzifer/webcheck` the tool.

## Usage

```console
$ webcheck --help
Usage of webcheck:
  -i, --interval duration        Check interval (default 1s)
  -l, --log-dir string           Directory to log non-matched requests to (default "/tmp/resp-log/")
      --log-retention duration   When to clean up file from log-dir (default 24h0m0s)
  -m, --match string             RegExp to match the response body against to validate it (default ".*")
      --no-log                   Disable response body logging
  -t, --timeout duration         Timeout for the request (default 30s)
  -u, --url string               URL to query
      --version                  Prints current version and exits
```

### Example

```console
$ webcheck -u https://bfa1c797.eu.ngrok.io/monitoring.txt -m healthy

[Mon, 23 Jul 2018 16:07:02 CEST] (OKAY) Status was 200 and text matched (13.331ms/14.229ms/115.599ms)
[Mon, 23 Jul 2018 16:07:16 CEST] (FAIL) Response body does not match regexp (13.314ms/14.229ms/15.316ms) (Resp: request-log/request827008143)
[Mon, 23 Jul 2018 16:07:21 CEST] (OKAY) Status was 200 and text matched (13.411ms/14.436ms/18.25ms)
[Mon, 23 Jul 2018 16:07:28 CEST] (FAIL) Status code was != 2xx: 404 (6.923ms/7.011ms/7.237ms) (Resp: request-log/request070057634)

$ cat request-log/request827008143
Accept-Ranges: bytes
Content-Length: 4
Content-Type: text/plain; charset=utf-8
Date: Mon, 23 Jul 2018 14:07:16 GMT
Last-Modified: Mon, 23 Jul 2018 14:07:15 GMT

foo
```
