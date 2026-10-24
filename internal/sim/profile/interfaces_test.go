package profile

import "testing"

// The simulator serves one SSH and one Telnet listener per device, so a
// profile declaring a second of either fails to load instead of serving
// only one of them. Several tcp interfaces are fine: each gets its own.
func TestValidateInterfaces(t *testing.T) {
	ifaces := func(protos ...string) DeviceProfile {
		var p DeviceProfile
		for i, pr := range protos {
			p.Interfaces = append(p.Interfaces, Interface{Name: pr + string(rune('a'+i)), Protocol: pr, Port: 1000 + i})
		}
		return p
	}
	for _, tc := range []struct {
		name    string
		p       DeviceProfile
		wantErr bool
	}{
		{"one of each", ifaces("http", "ssh", "telnet", "tcp", "snmp"), false},
		{"two tcp bands", ifaces("tcp", "tcp"), false},
		{"two http", ifaces("http", "http"), false},
		{"two ssh", ifaces("ssh", "ssh"), true},
		{"two telnet", ifaces("telnet", "telnet"), true},
		{"two snmp", ifaces("snmp", "snmp"), true},
	} {
		if err := validateInterfaces(&tc.p); (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
	}
}

// A certificate validity window shapes a face behind TLS (#1035): one on a
// face with no tls is a profile mistake, and so is a negative one.
func TestValidateInterfacesTLSValidity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		iface   Interface
		wantErr bool
	}{
		{"a tls line face with a window", Interface{Name: "a", Protocol: "tcp", Port: 1, TLS: true, TLSValidityDays: 10}, false},
		{"a tls line face, the default window", Interface{Name: "a", Protocol: "tcp", Port: 1, TLS: true}, false},
		{"a window with no tls", Interface{Name: "a", Protocol: "tcp", Port: 1, TLSValidityDays: 10}, true},
		{"a negative window", Interface{Name: "a", Protocol: "tcp", Port: 1, TLS: true, TLSValidityDays: -1}, true},
	} {
		p := DeviceProfile{Interfaces: []Interface{tc.iface}}
		if err := validateInterfaces(&p); (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
	}
}

// A datagram face's loss (#1037) is a share of datagrams, so it belongs to
// a udp face and lies between 0 and 100.
func TestValidateInterfacesLoss(t *testing.T) {
	for _, tc := range []struct {
		name    string
		iface   Interface
		wantErr bool
	}{
		{"a lossy udp face", Interface{Name: "a", Protocol: "udp", Port: 1, Persistent: true, LossPct: 30}, false},
		{"a lossless udp face", Interface{Name: "a", Protocol: "udp", Port: 1, Persistent: true}, false},
		{"loss on tcp", Interface{Name: "a", Protocol: "tcp", Port: 1, LossPct: 30}, true},
		{"a negative loss", Interface{Name: "a", Protocol: "udp", Port: 1, LossPct: -1}, true},
		{"over all of it", Interface{Name: "a", Protocol: "udp", Port: 1, LossPct: 101}, true},
	} {
		p := DeviceProfile{Interfaces: []Interface{tc.iface}}
		if err := validateInterfaces(&p); (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
	}
}

// A refused community (#821) is an snmp face's, and differs from the
// community the face answers.
func TestValidateInterfacesRefusedCommunity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		iface   Interface
		wantErr bool
	}{
		{"an snmp face refusing one", Interface{Name: "a", Protocol: "snmp", Port: 1, Auth: Auth{Community: "public", RefusedCommunity: "revoked"}}, false},
		{"refusing its own", Interface{Name: "a", Protocol: "snmp", Port: 1, Auth: Auth{Community: "public", RefusedCommunity: "public"}}, true},
		{"on another face", Interface{Name: "a", Protocol: "http", Port: 1, Auth: Auth{RefusedCommunity: "revoked"}}, true},
	} {
		p := DeviceProfile{Interfaces: []Interface{tc.iface}}
		if err := validateInterfaces(&p); (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
	}
}
