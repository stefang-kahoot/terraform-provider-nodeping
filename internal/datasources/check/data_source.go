package check

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/datasources/checkattr"
)

var _ datasource.DataSource = &CheckDataSource{}
var _ datasource.DataSourceWithConfigure = &CheckDataSource{}

type CheckDataSource struct {
	client *client.Client
}

// CheckDataSourceModel is the shared check shape. It is declared here rather
// than aliased so the tfsdk tags stay visible at the point of use.
type CheckDataSourceModel = checkattr.Model

func NewCheckDataSource() datasource.DataSource {
	return &CheckDataSource{}
}

func (d *CheckDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_check"
}

func (d *CheckDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := checkattr.Attributes()

	// The caller supplies the ID; everything else is read back.
	attrs["id"] = schema.StringAttribute{
		Description: "The unique identifier of the check.",
		Required:    true,
	}

	resp.Schema = schema.Schema{
		Description: "Fetches a NodePing check by ID.",
		MarkdownDescription: `
Fetches a NodePing check by ID, including the check-type specific parameters.

-> Credentials are not exposed. ` + "`password`" + ` and ` + "`snmpcom`" + ` are
omitted deliberately; ` + "`sshkey`" + ` and ` + "`clientcert`" + ` return
NodePing's identifier for a stored key, not the key material.

## Example Usage

` + "```hcl" + `
data "nodeping_check" "example" {
  id = "201205050153W2Q4C-0J2HSIRF"
}

output "check_target" {
  value = data.nodeping_check.example.target
}

output "expected_content" {
  value = data.nodeping_check.example.contentstring
}
` + "```" + `
`,
		Attributes: attrs,
	}
}

func (d *CheckDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *CheckDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config CheckDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()

	tflog.Debug(ctx, "Reading check data source", map[string]interface{}{"id": id})

	check, err := d.client.GetCheck(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Check",
			"Could not read check ID "+id+": "+err.Error(),
		)
		return
	}

	state := checkattr.FromAPI(ctx, check, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	// The API does not echo the ID for every check type, so keep the one the
	// caller asked for.
	if state.ID.IsNull() || state.ID.ValueString() == "" {
		state.ID = types.StringValue(id)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
