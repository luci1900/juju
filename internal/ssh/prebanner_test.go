// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package ssh

import (
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/juju/tc"
)

type preBannerSuite struct{}

func TestPreBannerSuite(t *testing.T) {
	tc.Run(t, &preBannerSuite{})
}

// readWritten reads everything written to the client side of a net.Pipe by
// the given write func, which is called on the server side.
func readWritten(c *tc.C, write func(conn net.Conn) error) string {
	client, server := net.Pipe()
	defer client.Close()

	writeErr := make(chan error, 1)
	go func() {
		defer server.Close()
		writeErr <- write(server)
	}()

	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	got, err := io.ReadAll(client)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(<-writeErr, tc.ErrorIsNil)
	return string(got)
}

func (*preBannerSuite) TestWritesCRLFTerminatedLine(c *tc.C) {
	got := readWritten(c, func(conn net.Conn) error {
		return WritePreBannerError(conn, "access denied")
	})
	c.Check(got, tc.Equals, "access denied\r\n")
}

func (*preBannerSuite) TestFlattensNewlinesToSpaces(c *tc.C) {
	got := readWritten(c, func(conn net.Conn) error {
		return WritePreBannerError(conn, "line one\nline two\nline three")
	})
	c.Check(got, tc.Equals, "line one line two line three\r\n")
	c.Check(strings.Count(got, "\n"), tc.Equals, 1) // only the trailing CRLF
}

func (*preBannerSuite) TestCapsMessageLength(c *tc.C) {
	long := strings.Repeat("x", 500)
	got := readWritten(c, func(conn net.Conn) error {
		return WritePreBannerError(conn, long)
	})
	c.Assert(got, tc.HasLen, 202) // 200 chars + "\r\n"
	c.Check(got, tc.Equals, strings.Repeat("x", 200)+"\r\n")
}

func (*preBannerSuite) TestEmptyMessage(c *tc.C) {
	got := readWritten(c, func(conn net.Conn) error {
		return WritePreBannerError(conn, "")
	})
	c.Check(got, tc.Equals, "\r\n")
}

func (*preBannerSuite) TestWriteErrorPropagated(c *tc.C) {
	client, server := net.Pipe()
	// Close the client side immediately so writes on server fail.
	c.Assert(client.Close(), tc.ErrorIsNil)
	defer server.Close()

	err := WritePreBannerError(server, "hello")
	c.Assert(err, tc.NotNil)
}
