package provider

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/hc-install/product"
	"github.com/hashicorp/hc-install/releases"
	"github.com/hashicorp/terraform-exec/tfexec"
)

// testVariables keep scenarios from reaching external services: quota
// polling and event or notification delivery are switched off.
var testVariables = map[string]string{
	"enable_quota_polling": "false",
	"enable_delivery":      "false",
}

func terraformBinary(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("TF_ACC_TERRAFORM_PATH"); p != "" {
		return p
	}
	if p, err := exec.LookPath("terraform"); err == nil {
		return p
	}
	inst := &releases.LatestVersion{Product: product.Terraform, InstallDir: t.TempDir(),
		Constraints: version.MustConstraints(version.NewConstraint(">= 1.11"))}
	p, err := inst.Install(context.Background())
	if err != nil {
		t.Fatalf("install terraform: %v", err)
	}
	return p
}

// providerBinary builds this provider and returns a CLI configuration that
// points Terraform at it with dev_overrides, so examples run unchanged
// (including their required_providers block).
func providerBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(dir, "terraform-provider-harbor-pilot"), ".") //nolint:gosec // test build
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build provider: %v\n%s", err, out)
	}
	rc := filepath.Join(dir, "terraformrc")
	cfg := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    \"aurimasniekis/harbor-pilot\" = %q\n  }\n  direct {}\n}\n", dir)
	if err := os.WriteFile(rc, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return rc
}

// TestAccScenarioL applies every example under examples/scenarios against a
// real server exactly as written, checks that a fresh plan is empty, and
// destroys it.
func TestAccScenarioL(t *testing.T) {
	if srv == nil {
		t.Skip("acceptance tests require TF_ACC=1")
	}
	dirs, err := filepath.Glob(filepath.Join("..", "..", "examples", "scenarios", "*"))
	if err != nil || len(dirs) != 7 {
		t.Fatalf("expected 7 scenario examples, found %d (%v)", len(dirs), err)
	}
	tf := terraformBinary(t)
	rc := providerBinary(t)
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			work := t.TempDir()
			files, _ := filepath.Glob(filepath.Join(dir, "*.tf"))
			var all bytes.Buffer
			for _, f := range files {
				data, err := os.ReadFile(f) //nolint:gosec // repository example
				if err != nil {
					t.Fatal(err)
				}
				all.Write(data)
				if err := os.WriteFile(filepath.Join(work, filepath.Base(f)), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			ex, err := tfexec.NewTerraform(work, tf)
			if err != nil {
				t.Fatal(err)
			}
			if v, _, err := ex.Version(context.Background(), true); err == nil {
				t.Logf("terraform %s", v)
			}
			env := map[string]string{"TF_CLI_CONFIG_FILE": rc, EnvServer: srv.URL, EnvToken: srv.Token}
			if err := ex.SetEnv(env); err != nil {
				t.Fatal(err)
			}
			var vars []string
			for name, v := range testVariables {
				if strings.Contains(all.String(), `variable "`+name+`"`) {
					vars = append(vars, name+"="+v)
				}
			}
			ctx := context.Background()
			applyOpts := []tfexec.ApplyOption{}
			planOpts := []tfexec.PlanOption{}
			destroyOpts := []tfexec.DestroyOption{}
			for _, v := range vars {
				applyOpts = append(applyOpts, tfexec.Var(v))
				planOpts = append(planOpts, tfexec.Var(v))
				destroyOpts = append(destroyOpts, tfexec.Var(v))
			}
			var stderr bytes.Buffer
			ex.SetStderr(&stderr)
			t.Cleanup(func() {
				if err := ex.Destroy(ctx, destroyOpts...); err != nil {
					t.Errorf("destroy: %v\n%s", err, stderr.String())
				}
			})
			if err := ex.Apply(ctx, applyOpts...); err != nil {
				t.Fatalf("apply: %v\n%s", err, stderr.String())
			}
			st, err := ex.Show(ctx)
			if err != nil {
				t.Fatalf("show: %v", err)
			}
			if st.Values == nil || len(st.Values.RootModule.Resources) < 5 {
				t.Fatalf("apply created too few resources: %+v", st.Values)
			}
			t.Logf("applied %d resources", len(st.Values.RootModule.Resources))
			changed, err := ex.Plan(ctx, planOpts...)
			if err != nil {
				t.Fatalf("plan: %v\n%s", err, stderr.String())
			}
			if changed {
				var out bytes.Buffer
				ex.SetStdout(&out)
				_, _ = ex.Plan(ctx, planOpts...)
				t.Fatalf("second plan is not empty:\n%s", out.String())
			}
		})
	}
}
