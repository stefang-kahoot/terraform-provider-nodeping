package contact

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

var _ datasource.DataSource = &ContactDataSource{}
var _ datasource.DataSourceWithConfigure = &ContactDataSource{}

type ContactDataSource struct {
	client *client.Client
}

type ContactDataSourceModel struct {
	ID         types.String             `tfsdk:"id"`
	CustomerID types.String             `tfsdk:"customer_id"`
	Name       types.String             `tfsdk:"name"`
	CustRole   types.String             `tfsdk:"custrole"`
	Addresses  []AddressDataSourceModel `tfsdk:"addresses"`
}

type AddressDataSourceModel struct {
	ID            types.String `tfsdk:"id"`
	Type          types.String `tfsdk:"type"`
	Address       types.String `tfsdk:"address"`
	SuppressUp    types.Bool   `tfsdk:"suppress_up"`
	SuppressDown  types.Bool   `tfsdk:"suppress_down"`
	SuppressFirst types.Bool   `tfsdk:"suppress_first"`
	SuppressDiag  types.Bool   `tfsdk:"suppress_diag"`
	SuppressAll   types.Bool   `tfsdk:"suppress_all"`
}

func NewContactDataSource() datasource.DataSource {
	return &ContactDataSource{}
}

func (d *ContactDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_contact"
}

func (d *ContactDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetches a NodePing contact by ID.",
		MarkdownDescription: `
Fetches a NodePing contact by ID.

## Example Usage

` + "```hcl" + `
data "nodeping_contact" "example" {
  id = "201205050153W2Q4C-BKPGH"
}

output "contact_name" {
  value = data.nodeping_contact.example.name
}
` + "```" + `
`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier of the contact.",
				Required:    true,
			},
			"customer_id": schema.StringAttribute{
				Description: "The customer ID (account ID) that owns this contact.",
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Description: "The name of the contact.",
				Computed:    true,
			},
			"custrole": schema.StringAttribute{
				Description: "The permission role for this contact.",
				Computed:    true,
			},
			"addresses": schema.ListNestedAttribute{
				Description: "Contact addresses for receiving notifications, ordered by ID.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "The unique identifier of the address.",
							Computed:    true,
						},
						"type": schema.StringAttribute{
							Description: "The type of address.",
							Computed:    true,
						},
						"address": schema.StringAttribute{
							Description: "The address value.",
							Computed:    true,
							Sensitive:   true,
						},
						"suppress_up": schema.BoolAttribute{
							Description: "Suppress 'up' notifications.",
							Computed:    true,
						},
						"suppress_down": schema.BoolAttribute{
							Description: "Suppress 'down' notifications.",
							Computed:    true,
						},
						"suppress_first": schema.BoolAttribute{
							Description: "Suppress 'first result' notifications.",
							Computed:    true,
						},
						"suppress_diag": schema.BoolAttribute{
							Description: "Suppress diagnostic notifications.",
							Computed:    true,
						},
						"suppress_all": schema.BoolAttribute{
							Description: "Suppress all notifications.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *ContactDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ContactDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config ContactDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Reading contact data source", map[string]interface{}{
		"id": config.ID.ValueString(),
	})

	contact, err := d.client.GetContact(ctx, config.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Contact",
			"Could not read contact ID "+config.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	config.CustomerID = types.StringValue(contact.CustomerID)
	config.Name = types.StringValue(contact.Name)
	config.CustRole = types.StringValue(contact.CustRole)

	// The API returns the addresses as a map, whose iteration order is random
	// in Go. Sorting by ID keeps the data source output stable between plans.
	ids := make([]string, 0, len(contact.Addresses))
	for id := range contact.Addresses {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	config.Addresses = make([]AddressDataSourceModel, 0, len(ids))
	for _, id := range ids {
		addr := contact.Addresses[id]
		config.Addresses = append(config.Addresses, AddressDataSourceModel{
			ID:            types.StringValue(id),
			Type:          types.StringValue(addr.Type),
			Address:       types.StringValue(addr.Address),
			SuppressUp:    types.BoolValue(addr.SuppressUp),
			SuppressDown:  types.BoolValue(addr.SuppressDown),
			SuppressFirst: types.BoolValue(addr.SuppressFirst),
			SuppressDiag:  types.BoolValue(addr.SuppressDiag),
			SuppressAll:   types.BoolValue(addr.SuppressAll),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
