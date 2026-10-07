package contactgroups

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
)

var _ datasource.DataSource = &ContactGroupsDataSource{}
var _ datasource.DataSourceWithConfigure = &ContactGroupsDataSource{}

type ContactGroupsDataSource struct {
	client *client.Client
}

type ContactGroupsDataSourceModel struct {
	Groups []ContactGroupModel `tfsdk:"contactgroups"`
}

type ContactGroupModel struct {
	ID         types.String `tfsdk:"id"`
	CustomerID types.String `tfsdk:"customer_id"`
	Name       types.String `tfsdk:"name"`
	Members    types.List   `tfsdk:"members"`
}

func NewContactGroupsDataSource() datasource.DataSource {
	return &ContactGroupsDataSource{}
}

func (d *ContactGroupsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_contactgroups"
}

func (d *ContactGroupsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetches all NodePing contact groups.",
		MarkdownDescription: `
Fetches all NodePing contact groups for the account.

## Example Usage

` + "```hcl" + `
data "nodeping_contactgroups" "all" {}

output "group_names" {
  value = [for g in data.nodeping_contactgroups.all.contactgroups : g.name]
}
` + "```" + `
`,
		Attributes: map[string]schema.Attribute{
			"contactgroups": schema.ListNestedAttribute{
				Description: "All contact groups, ordered by ID.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "The unique identifier of the contact group.",
							Computed:    true,
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
							Description: "Contact address IDs belonging to this group.",
							Computed:    true,
							ElementType: types.StringType,
						},
					},
				},
			},
		},
	}
}

func (d *ContactGroupsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ContactGroupsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config ContactGroupsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Reading contact groups data source")

	groups, err := d.client.ListContactGroups(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Contact Groups",
			"Could not list contact groups: "+err.Error(),
		)
		return
	}

	// The API returns a map, whose iteration order is random in Go. Sorting by
	// ID keeps the data source output stable between plans.
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	config.Groups = make([]ContactGroupModel, 0, len(ids))
	for _, id := range ids {
		group := groups[id]

		members := group.Members
		if members == nil {
			members = []string{}
		}
		list, diags := types.ListValueFrom(ctx, types.StringType, members)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		groupID := group.ID
		if groupID == "" {
			groupID = id
		}

		config.Groups = append(config.Groups, ContactGroupModel{
			ID:         types.StringValue(groupID),
			CustomerID: types.StringValue(group.CustomerID),
			Name:       types.StringValue(group.Name),
			Members:    list,
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
