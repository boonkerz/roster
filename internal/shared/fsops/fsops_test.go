package fsops

import "testing"

func TestParseOps(t *testing.T) {
	ops, err := ParseOps("# Kommentar\nmkdir \"/data/my vol\" mode=0775 owner=33:33\nchmod /data/x 755\nchown /data/x www-data:www-data\nfile /data/x/.keep = \"a = b\\nzwei\"\nfile /data/y = roh = text")
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 5 || ops[0].Path != "/data/my vol" || ops[0].Opts["mode"] != "0775" || ops[0].Opts["owner"] != "33:33" || ops[0].Line != 2 {
		t.Fatalf("%+v", ops[0])
	}
	if ops[1].Arg != "755" || ops[2].Arg != "www-data:www-data" || ops[3].Content != "a = b\nzwei" || ops[4].Content != "roh = text" {
		t.Fatalf("%+v", ops)
	}
	for _, bad := range []string{"rm /x", "mkdir /x mode=999", "mkdir /x foo=1", "chmod /x", "file /x", "mkdir \"/x"} {
		if _, err := ParseOps(bad); err == nil {
			t.Errorf("%q akzeptiert", bad)
		}
	}
}
