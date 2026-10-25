package session

import "testing"

func TestParseHop(t *testing.T) {
	cases := []struct{ in, target, sftp string }{
		{"ssh -t -i ~/.ssh/aws_key ec2-user@10.0.0.5", "ec2-user@10.0.0.5", "ssh -T -o BatchMode=yes -i ~/.ssh/aws_key -s ec2-user@10.0.0.5 sftp"},
		{"ssh -tt -p2222 -o StrictHostKeyChecking=no admin@h top", "admin@h", "ssh -T -o BatchMode=yes -p 2222 -o StrictHostKeyChecking=no -s admin@h sftp"},
		{"exec ssh -At -i '/k/my key' h", "h", "ssh -T -o BatchMode=yes -A -i '/k/my key' -s h sftp"},
		{"/usr/bin/ssh -J bastion2 u@x", "u@x", "/usr/bin/ssh -T -o BatchMode=yes -J bastion2 -s u@x sftp"},
	}
	for _, c := range cases {
		h, ok := ParseHop(c.in)
		if !ok || h.Target != c.target || h.SFTPCommand() != c.sftp {
			t.Errorf("%q → %v %q %q", c.in, ok, h.Target, h.SFTPCommand())
		}
	}
	for _, bad := range []string{"", "top", "sudo ssh h", "ssh -i", "ssh -t"} {
		if _, ok := ParseHop(bad); ok {
			t.Errorf("%q should not parse", bad)
		}
	}
}
