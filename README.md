# mdwiki-dev-server

[mdwiki](http://dynalon.github.io/mdwiki/#!index.md) is

> a CMS/Wiki completely built in HTML5/Javascript and runs 100% on the client.

It works great when the files (html, js, md, ...) are handed to it by
a server.  Some browsers are able to load pages using `file://` URL's
but others have trouble.

The standard workaround is to serve the files with something like:

    python -m SimpleHTTPServer 8080

This works well, but one needs to reload the page after every change.

I wanted to see if I could write a little server that would handle the
requests for the static files and that also would notify the browser
when it needed to reload the page (following in the footsteps of the
[LiveReload](http://livereload.com/) family of tools.

I hacked together a node.js server using
[node-live-reload](https://www.npmjs.org/package/node-live-reload))
and various libraries but my node-fu is weak, some of the libraries I
used didn't have clean interfaces for what I needed to do, and the
result was a bit of a hack.

I'm interested in getting better at [Go](http://golang.org) so I've
taken a whack at it.

It's my first project in Go, and it shows.  Caveat pretty-much-everything.
