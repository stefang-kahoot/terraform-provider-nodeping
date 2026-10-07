package contactgroup

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
)

var _ datasource.DataSource = &ContactGroupDataSource{}
var _ datasource.DataSourceWithConfigure = &ContactGroupDataSource{}

type ContactGroupDataSource struct {
	client *client.Client
}

type ContactGroupDataSourceModel struct {
	ID         types.String `tfsdk:"id"`
	CustomerID types.String `tfsdk:"customer_id"`
	Name       types.String `tfsdk:"name"`
	Members    types.List   `tfsdk:"members"`
}

func NewContactGroupDataSource() datasource.DataSource {
	return &ContactGroupDataSource{}
}

func (d *ContactGroupDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_contactgroup"
}

func (d *ContactGroupDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetches a NodePing contact group by ID.",
		MarkdownDescription: `
Fetches a NodePing contact group by ID.

## Example Usage

` + "```hcl" + `
data "nodeping_contactgroup" "escalation" {
  id = "201205050153W2Q4C-G-1ZIYU"
}

output "escalation_members" {
  value = data.nodeping_contactgroup.escalation.members
}
` + "```" + `
`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier of the contact group.",
				Required:    true,
			},
			"customer_id": schema.StringAttribute{
				Description: "The customer ID (account ID) that owns this contact group.",
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Description: "The display name of the contact group.",
				Computed:    true,
			},
			"members": schema.ListAttribute{
				Description:         "Contact address IDs belonging to this group.",
				MarkdownDescription: "Contact **address** IDs belonging to this group, not `nodeping_contact` IDs.",
				Computed:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

func (d *ContactGroupDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ContactGroupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config ContactGroupDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Reading contact group data source", map[string]interface{}{
		"id": config.ID.ValueString(),
	})

	group, err := d.client.GetContactGroup(ctx, config.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Contact Group",
			"Could not read contact group ID "+config.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	config.CustomerID = types.StringValue(group.CustomerID)
	config.Name = types.StringValue(group.Name)

	members := group.Members
	if members == nil {
		members = []string{}
	}
	list, diags := types.ListValueFrom(ctx, types.StringType, members)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	config.Members = list

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
