// Package topo reads the neighbour, port, LAG and spanning-tree state that network devices print and joins
// it into one map: devices (with a session or only seen by a neighbour), links with their state, and the
// changes between two reads. It only parses text and joins results; running the commands is the caller's job.
package topo

import "time"

// Proto is the discovery protocol a neighbour was learned with.
type Proto string

const (
	LLDP Proto = "lldp"
	CDP  Proto = "cdp"
	EDP  Proto = "edp"
)

// Neighbor is what one local port heard from the device on the other end of its cable.
type Neighbor struct {
	Proto      Proto  `json:"proto"`
	LocalPort  string `json:"localPort"`
	Chassis    string `json:"chassis,omitempty"` // MAC, lower case aa:bb:cc:dd:ee:ff, when it is one
	RemotePort string `json:"remotePort,omitempty"`
	Name       string `json:"name,omitempty"`
	IP         string `json:"ip,omitempty"`
	Caps       string `json:"caps,omitempty"`
	// Age is how many seconds ago it was last heard; -1 when the output does not say.
	Age int `json:"age"`
}

// Port is one port's state.
type Port struct {
	Name  string `json:"name"`
	Admin bool   `json:"admin"` // enabled
	Oper  bool   `json:"oper"`  // link up
	// STP: "forwarding", "blocked", "learning", "disabled" or "" (not known).
	STP   string `json:"stp,omitempty"`
	Speed string `json:"speed,omitempty"`
	// VLANs: the VLAN numbers the port is a member of (tagged or not); nil when not read.
	VLANs []int `json:"vlans,omitempty"`
}

// LAG is one link aggregation group on a device.
type LAG struct {
	Name    string   `json:"name"`
	Mode    string   `json:"mode"` // "lacp" or "static"
	Members []string `json:"members"`
	Active  []string `json:"active,omitempty"`
}

// Device is what was read from one device with a session.
type Device struct {
	Session string `json:"session"` // the session's path
	Name    string `json:"name"`
	// SysName: the name the device gives itself (what its neighbours call it); the session's name when unknown.
	SysName   string     `json:"sysName,omitempty"`
	OS        string     `json:"os"` // exos, voss, ios, nxos, eos, junos
	Chassis   string     `json:"chassis,omitempty"`
	Model     string     `json:"model,omitempty"` // "5520-24T-SwitchEngine" (show switch / show sys-info)
	IP        string     `json:"ip,omitempty"`
	Neighbors []Neighbor `json:"neighbors"`
	Ports     []Port     `json:"ports"`
	LAGs      []LAG      `json:"lags"`
	// VLANNames: VLAN number -> name, as the device calls it.
	VLANNames map[int]string `json:"vlanNames,omitempty"`
	// Notes: what could not be read ("LLDP is off", "show sharing failed"), shown on the node.
	Notes []string `json:"notes,omitempty"`
	// Err: the device could not be read (login refused, timed out). Its (empty) data is then not trusted:
	// links it had stay as they were.
	Err  string    `json:"err,omitempty"`
	Read time.Time `json:"read"`
}

// Node is a device on the map.
type Node struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	OS      string `json:"os,omitempty"`
	IP      string `json:"ip,omitempty"`
	Chassis string `json:"chassis,omitempty"`
	Session string `json:"session,omitempty"` // "" for a ghost
	// Known: a session of the Session Manager that is this device but was not read in this map.
	Known string   `json:"known,omitempty"`
	Ghost bool     `json:"ghost"`
	Caps  string   `json:"caps,omitempty"`
	Notes []string `json:"notes,omitempty"`
	// VLANs: the VLANs on the device's ports (read devices only).
	VLANs []int  `json:"vlans,omitempty"`
	Model string `json:"model,omitempty"`
	// PortsTotal / PortsUp: the device's ports and how many have link (read devices only).
	PortsTotal int `json:"portsTotal,omitempty"`
	PortsUp    int `json:"portsUp,omitempty"`
	// MgmtVLAN: the management VLAN (named like "Mgmt"), MgmtPort: the port in it ("" = out-of-band management port).
	MgmtVLAN  *VLANName `json:"mgmtVlan,omitempty"`
	MgmtPorts []string  `json:"mgmtPorts,omitempty"`
}

// Link is one cable.
type Link struct {
	Key   string `json:"key"`
	A     string `json:"a"`
	APort string `json:"aPort"`
	B     string `json:"b"`
	BPort string `json:"bPort"`
	// State: "up", "blocked" (spanning tree blocks an end) or "down" (kept from the last read while a port is down).
	State string `json:"state"`
	LagA  *LAG   `json:"lagA,omitempty"`
	LagB  *LAG   `json:"lagB,omitempty"`
	// Warn: the two ends disagree about the LAG (one side only, or static against LACP).
	Warn string  `json:"warn,omitempty"`
	STPA string  `json:"stpA,omitempty"`
	STPB string  `json:"stpB,omitempty"`
	Seen []Proto `json:"seen"` // the protocols that showed it
	// VLANs: the VLANs the cable carries (both ends members).
	VLANs []int  `json:"vlans,omitempty"`
	Ends  int    `json:"ends"` // 1 = one end saw it, 2 = both
	Speed string `json:"speed,omitempty"`
}

// Map is the joined result.
type Map struct {
	Nodes []Node     `json:"nodes"`
	Links []Link     `json:"links"`
	VLANs []VLANName `json:"vlans,omitempty"`
	At    time.Time  `json:"at"`
}

// Change is one difference between two maps.
type Change struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"` // link-up, link-down, moved, blocked, forwarding, lag, node-new, node-lost
	Text string    `json:"text"`
}
