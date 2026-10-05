VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X main.version=$(VERSION)

PREFIX ?= /usr/local
SBINDIR ?= $(PREFIX)/sbin
# OpenBSD and FreeBSD < 14: $(PREFIX)/man; FreeBSD >= 14: $(PREFIX)/share/man
MANDIR ?= $(PREFIX)/man
EXAMPLESDIR ?= $(PREFIX)/share/examples/zonefile-go
INSTALL ?= install

MANPAGES = zonefile-go.8 zonefile.conf.5

.PHONY: build test vet build-all build-openbsd build-freebsd build-linux \
        install uninstall man clean

build:
	go build -ldflags="$(LDFLAGS)" -o zonefile-go .

test:
	go vet ./...
	go test -race ./...

build-all: build-openbsd build-freebsd build-linux

build-openbsd:
	CGO_ENABLED=0 GOOS=openbsd GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o zonefile-go-openbsd-amd64 .

build-freebsd:
	CGO_ENABLED=0 GOOS=freebsd GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o zonefile-go-freebsd-amd64 .

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o zonefile-go-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o zonefile-go-linux-arm64 .

install: build
	$(INSTALL) -d $(DESTDIR)$(SBINDIR) $(DESTDIR)$(MANDIR)/man5 \
		$(DESTDIR)$(MANDIR)/man8 $(DESTDIR)$(EXAMPLESDIR)
	$(INSTALL) -m 755 zonefile-go $(DESTDIR)$(SBINDIR)/zonefile-go
	$(INSTALL) -m 444 zonefile-go.8 $(DESTDIR)$(MANDIR)/man8/zonefile-go.8
	$(INSTALL) -m 444 zonefile.conf.5 $(DESTDIR)$(MANDIR)/man5/zonefile.conf.5
	$(INSTALL) -m 644 examples/zones.conf $(DESTDIR)$(EXAMPLESDIR)/zonefile.conf

uninstall:
	rm -f $(DESTDIR)$(SBINDIR)/zonefile-go \
		$(DESTDIR)$(MANDIR)/man8/zonefile-go.8 \
		$(DESTDIR)$(MANDIR)/man5/zonefile.conf.5 \
		$(DESTDIR)$(EXAMPLESDIR)/zonefile.conf
	-rmdir $(DESTDIR)$(EXAMPLESDIR)

# Check and display the manual pages
man:
	mandoc -Tlint $(MANPAGES) || true
	mandoc -Tascii $(MANPAGES) | $${PAGER:-less}

clean:
	rm -f zonefile-go zonefile-go-*
