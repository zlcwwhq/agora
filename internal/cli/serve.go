package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/zlcwwhq/agora/internal/config"
	"github.com/zlcwwhq/agora/internal/post"
	"github.com/zlcwwhq/agora/internal/server"
	"github.com/zlcwwhq/agora/internal/store"
)

func runServe(c *ctx, _ []string) error {
	cfg, err := c.config()
	if err != nil {
		return err
	}
	addr := cfg.Addr()
	if c.o.addr != "" {
		addr = c.o.addr
	}
	db := c.o.db
	if db == "" {
		state, err := config.StateDir(c.env.Getenv, c.env.GOOS, c.home())
		if err != nil {
			return fmt.Errorf("%w; pass --db FILE", err)
		}
		if err := os.MkdirAll(state, 0o700); err != nil {
			return err
		}
		db = filepath.Join(state, "board.db")
	}
	// The forms act as a person only by the person's own choice: detection
	// names the agent session that started serve.
	person, _, err := c.identity(false)
	if err != nil {
		return err
	}
	if !person.IsZero() && !person.IsPerson() {
		c.warnf("the pages only read: their forms act only as a person, %s/NAME, not as %s", post.Person, person.Author())
		person = post.Identity{}
	}

	st, err := store.Open(db, c.env.Now)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	tcp, _ := ln.Addr().(*net.TCPAddr)
	if tcp == nil || !tcp.IP.IsLoopback() {
		c.warnf("listening on %s: anyone who reaches it can read the board and write to it as anyone", ln.Addr())
	}
	url := "http://" + displayAddr(ln.Addr()) + "/"
	author := ""
	if !person.IsZero() {
		author = person.Author()
	}
	if c.o.json {
		// One line, which callers wait for, so it goes out now.
		line, err := json.Marshal(map[string]string{"url": url, "db": db, "author": author})
		if err == nil {
			_, err = fmt.Fprintf(c.env.Stdout, "%s\n", line)
		}
		if err != nil {
			_ = ln.Close()
			return err
		}
	} else {
		fmt.Fprintf(c.env.Stdout, "agora serving %s\ndatabase %s\n", url, db)
		if author != "" {
			fmt.Fprintf(c.env.Stdout, "forms act as %s\n", author)
		} else {
			fmt.Fprintf(c.env.Stdout, "the pages only read; pass --as human/NAME for forms\n")
		}
	}

	ctx := c.env.Context
	if ctx == nil {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
	}
	err = server.Run(ctx, ln, server.Options{
		Board:   st,
		Person:  person,
		Version: c.env.Version,
		Now:     c.env.Now,
		Logf:    func(format string, args ...any) { fmt.Fprintf(c.env.Stderr, format+"\n", args...) },
	})
	if errors.Is(err, context.Canceled) {
		err = nil
	}
	return err
}

// displayAddr returns the address to show for a listener: a loopback address
// for one that listens on every address, which a browser can't open.
func displayAddr(a net.Addr) string {
	tcp, ok := a.(*net.TCPAddr)
	if !ok {
		return a.String()
	}
	ip := tcp.IP
	if ip.IsUnspecified() {
		if ip.To4() != nil {
			ip = net.IPv4(127, 0, 0, 1)
		} else {
			ip = net.IPv6loopback
		}
	}
	return net.JoinHostPort(ip.String(), fmt.Sprint(tcp.Port))
}
