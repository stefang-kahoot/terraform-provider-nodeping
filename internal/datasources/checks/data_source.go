package checks

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/datasources/checkattr"
)

var _ datasource.DataSource = &ChecksDataSource{}
var _ datasource.DataSourceWithConfigure = &ChecksDataSource{}

type ChecksDataSource struct {
	client *client.Client
}

type ChecksDataSourceModel struct {
	Type   types.String `tfsdk:"type"`
	Checks []CheckModel `tfsdk:"checks"`
}

// CheckModel is the same shape the singular data source returns, so a check
// found through the list can be used exactly like one fetched by ID.
type CheckModel = checkattr.Model

func NewChecksDataSource() datasource.DataSource {
	return &ChecksDataSource{}
}

func (d *ChecksDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_checks"
}

func (d *ChecksDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetches all NodePing checks, optionally filtered by type.",
		MarkdownDescription: `
Fetches all NodePing checks, optionally filtered by type. Each entry carries the
same attributes as the ` + "`nodeping_check`" + ` data source, including the
check-type specific parameters.

-> Credentials are not exposed. ` + "`password`" + ` and ` + "`snmpcom`" + ` are
omitted deliberately; ` + "`sshkey`" + ` and ` + "`clientcert`" + ` return
NodePing's identifier for a stored key, not the key material.

## Example Usage

` + "```hcl" + `
data "nodeping_checks" "http" {
  type = "HTTP"
}

output "targets" {
  value = [for c in data.nodeping_checks.http.checks : c.target]
}

# Checks that follow redirects
output "following" {
  value = [
    for c in data.nodeping_checks.http.checks : c.label
    if c.follow == true
  ]
}
` + "```" + `
`,
		Attributes: map[string]schema.Attribute{
			"type": schema.StringAttribute{
				Description: "Filter checks by type.",
				Optional:    true,
			},
			"checks": schema.ListNestedAttribute{
				Description: "Matching checks, ordered by ID.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: checkattr.Attributes(),
				},
			},
		},
	}
}

func (d *ChecksDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T.", req.ProviderData),
		)
		return
	}

	d.client = c
}

func (d *ChecksDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config ChecksDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Reading checks data source")

	checks, err := d.client.ListChecks(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Checks",
			"Could not list checks: "+err.Error(),
		)
		return
	}

	typeFilter := config.Type.ValueString()

	// The API returns a map, and Go randomises map iteration. Without sorting,
	// the list order changes between reads and every plan shows a diff.
	ids := make([]string, 0, len(checks))
	for id := range checks {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	config.Checks = make([]CheckModel, 0, len(ids))
	for _, id := range ids {
		check := checks[id]
		if typeFilter != "" && check.Type != typeFilter {
			continue
		}

		model := checkattr.FromAPI(ctx, &check, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		if model.ID.IsNull() || model.ID.ValueString() == "" {
			model.ID = types.StringValue(id)
		}

		config.Checks = append(config.Checks, model)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
