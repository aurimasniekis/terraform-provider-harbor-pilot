package provider

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/acctest"
)

// srv is the shared server for acceptance tests (nil unless TF_ACC is set).
var srv *acctest.Server

var protoV6 = map[string]func() (tfprotov6.ProviderServer, error){
	"harborpilot": providerserver.NewProtocol6WithError(New("test")()),
}

func TestMain(m *testing.M) {
	if os.Getenv("TF_ACC") == "" {
		os.Exit(m.Run())
	}
	var err error
	srv, err = acctest.Start(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "start harbor-pilot:", err)
		os.Exit(1)
	}
	_ = os.Setenv(EnvServer, srv.URL)
	_ = os.Setenv(EnvToken, srv.Token)
	code := m.Run()
	if code != 0 {
		fmt.Fprintln(os.Stderr, "server log tail:\n"+srv.Log())
	}
	srv.Stop()
	os.Exit(code)
}

// providerBlock is an explicit provider configuration (values come from
// the environment set in TestMain).
const providerBlock = `provider "harborpilot" {}
`
