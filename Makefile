VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X main.version=$(VERSION)

PREFIX ?= /usr/local
SBINDIR ?= $(PREFIX)/sbin
# OpenBSD and FreeBSD < 14: $(PREFIX)/man; FreeBSD >= 14: $(PREFIX)/share/man
MANDIR ?= $(PREFIX)/man
EXAMPLESDIR ?= $(PREFIX)/share/examples/zonefile-go
INSTALL ?= install

MANPAGES = zonefile-go.8 zonefile.conf.5

# Archives for installation under /usr/local, built on any system:
# make dist-openbsd, make dist-freebsd; DIST_ARCH=arm64 for arm64.
DIST_ARCH ?= amd64
DISTDIR = dist
DIST_NAME = zonefile-go-$(VERSION)-$(DIST_OS)-$(DIST_ARCH)
DIST_STAGE = $(DISTDIR)/$(DIST_NAME)
# Files in the archive belong to root, whatever tar packs them.
DIST_OWNER = $(shell tar --version 2>/dev/null | grep -q bsdtar && \
	echo --uid 0 --gid 0 --uname root --gname wheel --no-xattrs || \
	echo --owner=0 --group=0 --numeric-owner)

.PHONY: build test vet build-all build-openbsd build-freebsd build-linux \
        dist dist-openbsd dist-freebsd install uninstall man clean

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

dist-openbsd:
	@$(MAKE) --no-print-directory dist DIST_OS=openbsd DIST_MANDIR=man \
		DIST_CONF=/etc/zonefile.conf

# FreeBSD keeps the configuration of ports in /usr/local/etc and, since 14,
# the manual pages in /usr/local/share/man.
dist-freebsd:
	@$(MAKE) --no-print-directory dist DIST_OS=freebsd DIST_MANDIR=share/man \
		DIST_CONF=/usr/local/etc/zonefile.conf

# dist packs the binary for DIST_OS and DIST_ARCH, the manual pages with
# DIST_CONF as the configuration path, and the example configuration in the
# layout of /usr/local. Unpack with tar -xzf FILE -C /usr/local.
dist:
	@test -n "$(DIST_OS)" || { echo "use make dist-openbsd or make dist-freebsd" >&2; exit 1; }
	rm -rf $(DIST_STAGE)
	mkdir -p $(DIST_STAGE)/sbin $(DIST_STAGE)/$(DIST_MANDIR)/man5 \
		$(DIST_STAGE)/$(DIST_MANDIR)/man8 $(DIST_STAGE)/share/examples/zonefile-go
	CGO_ENABLED=0 GOOS=$(DIST_OS) GOARCH=$(DIST_ARCH) go build -ldflags="$(LDFLAGS)" \
		-o $(DIST_STAGE)/sbin/zonefile-go .
	sed 's|/etc/zonefile\.conf|$(DIST_CONF)|g' zonefile-go.8 > $(DIST_STAGE)/$(DIST_MANDIR)/man8/zonefile-go.8
	sed 's|/etc/zonefile\.conf|$(DIST_CONF)|g' zonefile.conf.5 > $(DIST_STAGE)/$(DIST_MANDIR)/man5/zonefile.conf.5
	cp examples/zones.conf $(DIST_STAGE)/share/examples/zonefile-go/zonefile.conf
	find $(DIST_STAGE) -type d -exec chmod 755 {} +
	chmod 755 $(DIST_STAGE)/sbin/zonefile-go
	chmod 444 $(DIST_STAGE)/$(DIST_MANDIR)/man*/*
	chmod 644 $(DIST_STAGE)/share/examples/zonefile-go/zonefile.conf
	COPYFILE_DISABLE=1 tar -czf $(DISTDIR)/$(DIST_NAME).tgz $(DIST_OWNER) \
		-C $(DIST_STAGE) $(sort sbin share $(firstword $(subst /, ,$(DIST_MANDIR))))
	rm -rf $(DIST_STAGE)
	@echo $(DISTDIR)/$(DIST_NAME).tgz

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
	rm -rf $(DISTDIR)
