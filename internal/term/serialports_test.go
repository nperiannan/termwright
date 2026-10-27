package term

import (
	"reflect"
	"testing"
)

func TestCleanPortsOrder(t *testing.T) {
	in := []SerialPort{{Name: "COM10"}, {Name: "COM2"}, {Name: "com1"}, {Name: "/dev/ttyUSB10"}, {Name: "/dev/ttyUSB2"}, {Name: "/dev/ttyACM0"}}
	var got []string
	for _, p := range cleanPorts(in, "linux") {
		got = append(got, p.Name)
	}
	want := []string{"/dev/ttyACM0", "/dev/ttyUSB2", "/dev/ttyUSB10", "com1", "COM2", "COM10"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	mac := cleanPorts([]SerialPort{{Name: "/dev/tty.usbserial-1410"}, {Name: "/dev/cu.usbserial-1410"}, {Name: "/dev/cu.Bluetooth-Incoming-Port"}}, "darwin")
	if len(mac) != 2 || mac[0].Name != "/dev/cu.Bluetooth-Incoming-Port" || mac[1].Name != "/dev/cu.usbserial-1410" {
		t.Fatalf("mac ports: %v", mac)
	}
}

func TestBestBaud(t *testing.T) {
	if readable([]byte("\r\nswitch:1>")) != 1 {
		t.Fatal("prompt should be readable")
	}
	if r := readable([]byte{0x00, 0xf8, 0x80, 0xfe, 'a'}); r > 0.3 {
		t.Fatalf("garbage scored %v", r)
	}
	res := []BaudResult{{9600, 5, 0.2}, {115200, 40, 1}, {19200, 2, 1}, {38400, 0, 0}}
	if b := bestBaud(res); b != 115200 {
		t.Fatalf("got %d", b)
	}
	if b := bestBaud([]BaudResult{{9600, 0, 0}, {115200, 1, 1}}); b != 0 {
		t.Fatalf("nothing readable should give 0, got %d", b)
	}
}
