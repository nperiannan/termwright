package term

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
)

// setupX11 requests X11 forwarding and connects forwarded channels to the
// local X server ($DISPLAY; XQuartz on macOS, VcXsrv/X410 on Windows).
func setupX11(client *ssh.Client, sess *ssh.Session) error {
	display := os.Getenv("DISPLAY")
	if display == "" {
		if runtime.GOOS == "windows" {
			display = "localhost:0"
		} else {
			return errors.New("DISPLAY is not set (is an X server such as XQuartz running?)")
		}
	}
	screen := uint32(0)
	if i := strings.LastIndex(display, "."); i > strings.LastIndex(display, ":") {
		if n, err := strconv.Atoi(display[i+1:]); err == nil {
			screen = uint32(n)
		}
	}
	cookie := realCookie(display)
	if cookie == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		cookie = hex.EncodeToString(b)
	}
	chans := client.HandleChannelOpen("x11")
	if chans == nil {
		return errors.New("x11 channel handler already registered")
	}
	go func() {
		for nc := range chans {
			ch, reqs, err := nc.Accept()
			if err != nil {
				continue
			}
			go ssh.DiscardRequests(reqs)
			go func() {
				local, err := dialDisplay(display)
				if err != nil {
					ch.Close()
					return
				}
				pipeChannel(ch, local)
			}()
		}
	}()
	payload := struct {
		SingleConnection bool
		AuthProtocol     string
		AuthCookie       string
		ScreenNumber     uint32
	}{false, "MIT-MAGIC-COOKIE-1", cookie, screen}
	ok, err := sess.SendRequest("x11-req", true, ssh.Marshal(&payload))
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("server refused X11 forwarding")
	}
	return nil
}

func pipeChannel(ch ssh.Channel, c net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = copyBuf(c, ch); done <- struct{}{} }()
	go func() { _, _ = copyBuf(ch, c); done <- struct{}{} }()
	<-done
	ch.Close()
	c.Close()
}

func copyBuf(dst interface{ Write([]byte) (int, error) }, src interface{ Read([]byte) (int, error) }) (int64, error) {
	buf := make([]byte, 32*1024)
	var n int64
	for {
		r, err := src.Read(buf)
		if r > 0 {
			w, werr := dst.Write(buf[:r])
			n += int64(w)
			if werr != nil {
				return n, werr
			}
		}
		if err != nil {
			return n, err
		}
	}
}

// dialDisplay connects to an X display string like ":0", "localhost:10.0"
// or a launchd socket path ("/private/tmp/com.apple.launchd.x/org.xquartz:0").
func dialDisplay(display string) (net.Conn, error) {
	if strings.HasPrefix(display, "/") {
		if i := strings.LastIndex(display, ":"); i > 0 {
			return net.Dial("unix", display[:i])
		}
		return net.Dial("unix", display)
	}
	i := strings.LastIndex(display, ":")
	if i < 0 {
		return nil, errors.New("invalid DISPLAY")
	}
	host, num := display[:i], display[i+1:]
	if j := strings.Index(num, "."); j >= 0 {
		num = num[:j]
	}
	n, err := strconv.Atoi(num)
	if err != nil {
		return nil, errors.New("invalid DISPLAY")
	}
	if host == "" || host == "unix" {
		if runtime.GOOS != "windows" {
			return net.Dial("unix", "/tmp/.X11-unix/X"+num)
		}
		host = "127.0.0.1"
	}
	return net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(6000+n)))
}

// realCookie returns the local X server's MIT-MAGIC-COOKIE-1 via xauth, if any.
func realCookie(display string) string {
	out, err := exec.Command("xauth", "list", display).Output()
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) == 3 && f[1] == "MIT-MAGIC-COOKIE-1" {
			return f[2]
		}
	}
	return ""
}
