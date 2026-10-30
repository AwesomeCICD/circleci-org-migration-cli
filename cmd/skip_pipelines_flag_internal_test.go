package cmd

import "testing"

func TestSkipPipelinesFlag_RegisteredOnSyncAndMigrate(t *testing.T) {
	root := MakeCommands()
	for _, name := range []string{"sync", "migrate"} {
		sub, _, err := root.Find([]string{name})
		if err != nil || sub == nil || sub.Name() != name {
			t.Fatalf("%s command not found: %v", name, err)
		}
		f := sub.Flags().Lookup("skip-pipelines")
		if f == nil {
			t.Fatalf("%s: --skip-pipelines not registered", name)
		}
		if f.DefValue != "false" {
			t.Errorf("%s: --skip-pipelines must default to false, got %q", name, f.DefValue)
		}
	}
}
