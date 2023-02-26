package discord

import (
	"testing"

	"github.com/bwmarrin/snowflake"
)

func TestSnowflake(t *testing.T) {
	id, err := snowflake.ParseString("1067911534862139392")
	if err != nil {
		t.Error(err)
	}
	t.Error(id.Node())
}

func TestMarshalSuperProperties(t *testing.T) {
	want := SuperProperties{
		raw: "eyJvcyI6IldpbmRvd3MiLCJicm93c2VyIjoiQ2hyb21lIiwiZGV2aWNlIjoiIiwic3lzdGVtX2xvY2FsZSI6ImVzLUVTIiwiYnJvd3Nlcl91c2VyX2FnZW50IjoiTW96aWxsYS81LjAgKFdpbmRvd3MgTlQgMTAuMDsgV09XNjQpIEFwcGxlV2ViS2l0LzUzNy4zNiAoS0hUTUwsIGxpa2UgR2Vja28pIENocm9tZS8xMDkuMC4wLjAgU2FmYXJpLzUzNy4zNiIsImJyb3dzZXJfdmVyc2lvbiI6IjEwOS4wLjAuMCIsIm9zX3ZlcnNpb24iOiIxMCIsInJlZmVycmVyIjoiIiwicmVmZXJyaW5nX2RvbWFpbiI6IiIsInJlZmVycmVyX2N1cnJlbnQiOiIiLCJyZWZlcnJpbmdfZG9tYWluX2N1cnJlbnQiOiIiLCJyZWxlYXNlX2NoYW5uZWwiOiJzdGFibGUiLCJjbGllbnRfYnVpbGRfbnVtYmVyIjoxNzE2MjEsImNsaWVudF9ldmVudF9zb3VyY2UiOm51bGx9",
	}
	if err := want.Unmarshal(); err != nil {
		t.Fatal(err)
	}

	got := want
	got.raw = ""
	if err := got.Marshal(); err != nil {
		t.Fatal(err)
	}
	if got.raw != want.raw {
		t.Errorf("got %s, want %s", got.raw, want.raw)
	}
}
