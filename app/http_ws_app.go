package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/termwright/termwright/internal/apiclient"
)

// WebSocket requests of the API client: connect, send, and the messages as "api:ws" events; and the
// methods of a gRPC server or .proto files.

var (
	wsConns sync.Map // id -> *apiclient.WSConn
	wsSeq   atomic.Int64
)

// HTTPWSEvent is a message of an open WebSocket (event "api:ws").
type HTTPWSEvent struct {
	ID  string              `json:"id"`
	Msg apiclient.WSMessage `json:"msg"`
	// Closed: the connection ended (Msg says why).
	Closed bool `json:"closed,omitempty"`
}

// HTTPWSOpen is an opened WebSocket: its id and the handshake.
type HTTPWSOpen struct {
	ID        string             `json:"id"`
	Handshake apiclient.Response `json:"handshake"`
}

func (a *App) wsEmit(e HTTPWSEvent) {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "api:ws", e)
	}
}

// HTTPWSConnect opens a WebSocket with the request's URL (ws:// or wss://), headers, variables and
// authorization; the handshake comes back, the messages come as "api:ws" events.
func (a *App) HTTPWSConnect(r HTTPSendRequest) (HTTPWSOpen, error) {
	t, _, prod, err := a.httpTarget(r)
	if err != nil {
		return HTTPWSOpen{}, err
	}
	if prod && !r.Confirmed {
		return HTTPWSOpen{}, errHTTPConfirm
	}
	if a.ctx == nil {
		return HTTPWSOpen{}, errors.New("not available")
	}
	c, hs, err := apiclient.DialWS(a.ctx, r.Request, t)
	if err != nil {
		return HTTPWSOpen{Handshake: hs}, err
	}
	id := "ws" + strconv.FormatInt(wsSeq.Add(1), 10)
	wsConns.Store(id, c)
	go func() {
		why := c.Read(func(m apiclient.WSMessage) { a.wsEmit(HTTPWSEvent{ID: id, Msg: m}) })
		wsConns.Delete(id)
		a.wsEmit(HTTPWSEvent{ID: id, Closed: true, Msg: apiclient.WSMessage{Dir: "info", Text: why}})
	}()
	return HTTPWSOpen{ID: id, Handshake: hs}, nil
}

// HTTPWSSend sends a message on an open WebSocket (binary: the text is base64); variables are filled in.
func (a *App) HTTPWSSend(id, text string, binary bool, vars map[string]string) (apiclient.WSMessage, error) {
	v, ok := wsConns.Load(id)
	if !ok {
		return apiclient.WSMessage{}, errors.New("the WebSocket is not open")
	}
	if strings.Contains(text, "{{") {
		text = apiclient.Resolve(text, vars)
	}
	return v.(*apiclient.WSConn).Send(text, binary)
}

// HTTPWSPing sends a ping (the pong comes as an event).
func (a *App) HTTPWSPing(id string) error {
	v, ok := wsConns.Load(id)
	if !ok {
		return errors.New("the WebSocket is not open")
	}
	return v.(*apiclient.WSConn).Ping()
}

// HTTPWSClose closes a WebSocket.
func (a *App) HTTPWSClose(id string) error {
	if v, ok := wsConns.Load(id); ok {
		return v.(*apiclient.WSConn).Close()
	}
	return nil
}

// HTTPGRPCDescribe lists the methods of the gRPC server (its reflection) or of the request's .proto files.
func (a *App) HTTPGRPCDescribe(r HTTPSendRequest) (apiclient.GRPCSchema, error) {
	t, _, _, err := a.httpTarget(r)
	if err != nil {
		return apiclient.GRPCSchema{}, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return apiclient.GRPCDescribe(ctx, r.Request, t)
}
