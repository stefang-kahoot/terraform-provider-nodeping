package contact

import (
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// orderHook, orderEmail and orderEmpty are addresses as state holds them:
// one with every attribute set, one with every optional attribute null, and
// one with empty headers and query strings, as `headers = {}` reads back.
func orderHook() AddressModel {
	return AddressModel{
		ID:            types.StringValue("id-1"),
		Type:          types.StringValue("webhook"),
		Address:       types.StringValue("https://hooks.example.com/one"),
		SuppressUp:    types.BoolValue(true),
		SuppressDown:  types.BoolValue(false),
		SuppressFirst: types.BoolValue(true),
		SuppressDiag:  types.BoolValue(false),
		SuppressAll:   types.BoolValue(false),
		Mute:          types.BoolValue(true),
		Action:        types.StringValue("post"),
		Headers:       orderMap(map[string]string{"content-type": "application/json"}),
		QueryStrings:  orderMap(map[string]string{"key": "1"}),
		Data:          types.StringValue(`{"text":"{label} is {event}"}`),
		Priority:      types.Int64Value(1),
	}
}

func orderEmail() AddressModel {
	return AddressModel{
		ID:            types.StringValue("id-2"),
		Type:          types.StringValue("email"),
		Address:       types.StringValue("a@example.com"),
		SuppressUp:    types.BoolValue(false),
		SuppressDown:  types.BoolValue(false),
		SuppressFirst: types.BoolValue(false),
		SuppressDiag:  types.BoolValue(false),
		SuppressAll:   types.BoolValue(false),
		Mute:          types.BoolValue(false),
		Action:        types.StringNull(),
		Headers:       types.MapNull(types.StringType),
		QueryStrings:  types.MapNull(types.StringType),
		Data:          types.StringNull(),
		Priority:      types.Int64Null(),
	}
}

func orderEmpty() AddressModel {
	a := orderEmail()
	a.ID = types.StringValue("id-3")
	a.Type = types.StringValue("webhook")
	a.Address = types.StringValue("https://hooks.example.com/two")
	a.Headers = orderMap(map[string]string{})
	a.QueryStrings = orderMap(map[string]string{})
	return a
}

// orderMap is a map of strings as state holds it.
func orderMap(m map[string]string) types.Map {
	elems := make(map[string]attr.Value, len(m))
	for k, v := range m {
		elems[k] = types.StringValue(v)
	}
	return types.MapValueMust(types.StringType, elems)
}

// orderPrior is a contact as state holds it, its addresses in ID order as an
// import reads them.
func orderPrior() ContactResourceModel {
	return ContactResourceModel{
		ID:         types.StringValue("contact-1"),
		CustomerID: types.StringValue("customer-1"),
		Name:       types.StringValue("ops"),
		CustRole:   types.StringValue("notify"),
		Addresses:  []AddressModel{orderHook(), orderEmail(), orderEmpty()},
	}
}

// orderPlan is orderPrior with its addresses in another order, every one at a
// new position: email, empty, hook.
func orderPlan() ContactResourceModel {
	plan := orderPrior()
	plan.Addresses = []AddressModel{orderEmail(), orderEmpty(), orderHook()}
	return plan
}

func TestOnlyAddressOrderChanged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(plan, prior *ContactResourceModel)
		want   bool
	}{
		{"the order alone", func(_, _ *ContactResourceModel) {}, true},
		{"nothing at all", func(plan, _ *ContactResourceModel) { *plan = orderPrior() }, true},
		{"two identical addresses swapped, told apart by ID", func(plan, prior *ContactResourceModel) {
			second := orderEmail()
			second.ID = types.StringValue("id-4")
			prior.Addresses = []AddressModel{orderEmail(), second}
			plan.Addresses = []AddressModel{second, orderEmail()}
		}, true},
		{"a contact without addresses", func(plan, prior *ContactResourceModel) {
			plan.Addresses, prior.Addresses = nil, nil
		}, true},

		// What the plan cannot skip.
		{"the contact renamed", func(plan, _ *ContactResourceModel) { plan.Name = types.StringValue("ops-2") }, false},
		{"a name removed: null is not the empty name NodePing returned", func(plan, prior *ContactResourceModel) {
			prior.Name = types.StringValue("")
			plan.Name = types.StringNull()
		}, false},
		{"custrole changed", func(plan, _ *ContactResourceModel) { plan.CustRole = types.StringValue("view") }, false},
		{"an address added", func(plan, _ *ContactResourceModel) {
			added := orderEmail()
			added.ID = types.StringUnknown()
			added.Address = types.StringValue("b@example.com")
			plan.Addresses = append(plan.Addresses, added)
		}, false},
		{"an address removed", func(plan, _ *ContactResourceModel) { plan.Addresses = plan.Addresses[1:] }, false},
		{"an address replaced by a new one", func(plan, _ *ContactResourceModel) {
			plan.Addresses[0].ID = types.StringUnknown()
		}, false},
		{"an ID the prior state does not have", func(plan, _ *ContactResourceModel) {
			plan.Addresses[0].ID = types.StringValue("id-9")
		}, false},
		{"two blocks with the same ID", func(plan, _ *ContactResourceModel) {
			plan.Addresses[1] = plan.Addresses[0]
		}, false},
		{"a prior address without an ID", func(plan, prior *ContactResourceModel) {
			prior.Addresses[1].ID = types.StringNull()
			plan.Addresses[0].ID = types.StringNull()
		}, false},
		{"a prior address with an empty ID", func(plan, prior *ContactResourceModel) {
			prior.Addresses[1].ID = types.StringValue("")
			plan.Addresses[0].ID = types.StringValue("")
		}, false},
		{"two prior addresses with the same ID", func(plan, prior *ContactResourceModel) {
			prior.Addresses[2].ID = prior.Addresses[1].ID
			plan.Addresses[1].ID = prior.Addresses[1].ID
		}, false},
		{"data that is the same JSON written differently", func(plan, _ *ContactResourceModel) {
			plan.Addresses[2].Data = types.StringValue(`{"text": "{label} is {event}"}`)
		}, false},
		{"empty headers where state has none", func(plan, _ *ContactResourceModel) {
			plan.Addresses[0].Headers = orderMap(map[string]string{})
		}, false},
		{"no headers where state has empty ones", func(plan, _ *ContactResourceModel) {
			plan.Addresses[1].Headers = types.MapNull(types.StringType)
		}, false},
		{"a header name in another case", func(plan, _ *ContactResourceModel) {
			plan.Addresses[2].Headers = orderMap(map[string]string{"Content-Type": "application/json"})
		}, false},
		{"a header value not known until apply", func(plan, _ *ContactResourceModel) {
			plan.Addresses[2].Headers = types.MapValueMust(types.StringType, map[string]attr.Value{"content-type": types.StringUnknown()})
		}, false},
		{"priority 0 where state has none", func(plan, _ *ContactResourceModel) {
			plan.Addresses[0].Priority = types.Int64Value(0)
		}, false},
		{"mute switched off", func(plan, _ *ContactResourceModel) { plan.Addresses[2].Mute = types.BoolValue(false) }, false},
		{"a value unknown in both", func(plan, prior *ContactResourceModel) {
			prior.Addresses[0].Action = types.StringUnknown()
			plan.Addresses[2].Action = types.StringUnknown()
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			plan, prior := orderPlan(), orderPrior()
			tt.change(&plan, &prior)
			if got := onlyAddressOrderChanged(plan, prior); got != tt.want {
				t.Errorf("onlyAddressOrderChanged = %v, want %v", got, tt.want)
			}
		})
	}
}

// Every attribute of the contact and of each address counts: changing any of
// them in the plan, to any other value -- null, empty, unknown or another
// value -- is a change to send. The attributes are walked by reflection, so
// one added to the model without a comparison fails here.
func TestOnlyAddressOrderChangedSeesEveryAttribute(t *testing.T) {
	t.Parallel()

	if !onlyAddressOrderChanged(orderPlan(), orderPrior()) {
		t.Fatal("the reordered plan is taken for a change; the near misses below prove nothing")
	}

	contactType := reflect.TypeFor[ContactResourceModel]()
	for i := range contactType.NumField() {
		field := contactType.Field(i)
		if field.Name == "Addresses" {
			continue
		}
		prior := reflect.ValueOf(orderPrior()).Field(i).Interface().(attr.Value)
		for _, miss := range nearMisses(t, prior) {
			plan := orderPlan()
			reflect.ValueOf(&plan).Elem().Field(i).Set(reflect.ValueOf(miss))
			if onlyAddressOrderChanged(plan, orderPrior()) {
				t.Errorf("%s changed from %s to %s is not seen", field.Name, prior, miss)
			}
		}
	}

	addressType := reflect.TypeFor[AddressModel]()
	for pos, addr := range orderPlan().Addresses {
		for i := range addressType.NumField() {
			field := addressType.Field(i)
			prior := reflect.ValueOf(addr).Field(i).Interface().(attr.Value)
			for _, miss := range nearMisses(t, prior) {
				plan := orderPlan()
				reflect.ValueOf(&plan.Addresses[pos]).Elem().Field(i).Set(reflect.ValueOf(miss))
				if onlyAddressOrderChanged(plan, orderPrior()) {
					t.Errorf("%s of address %s changed from %s to %s is not seen", field.Name, addr.ID, prior, miss)
				}
			}
		}
	}
}

// nearMisses returns values of v's type that state would not hold as v.
func nearMisses(t *testing.T, v attr.Value) []attr.Value {
	t.Helper()

	switch v := v.(type) {
	case types.String:
		if v.IsNull() {
			return []attr.Value{types.StringValue(""), types.StringUnknown()}
		}
		misses := []attr.Value{types.StringNull(), types.StringUnknown(), types.StringValue(v.ValueString() + " ")}
		if v.ValueString() != "" {
			misses = append(misses, types.StringValue(""))
		}
		return misses
	case types.Bool:
		if v.IsNull() {
			return []attr.Value{types.BoolValue(false), types.BoolUnknown()}
		}
		return []attr.Value{types.BoolNull(), types.BoolUnknown(), types.BoolValue(!v.ValueBool())}
	case types.Int64:
		if v.IsNull() {
			return []attr.Value{types.Int64Value(0), types.Int64Unknown()}
		}
		return []attr.Value{types.Int64Null(), types.Int64Unknown(), types.Int64Value(v.ValueInt64() + 1)}
	case types.Map:
		misses := []attr.Value{types.MapUnknown(types.StringType)}
		if !v.IsNull() {
			misses = append(misses, types.MapNull(types.StringType))
		}
		entries := make(map[string]string, len(v.Elements()))
		for k, e := range v.Elements() {
			entries[k] = e.(types.String).ValueString()
		}
		if v.IsNull() || len(entries) > 0 {
			misses = append(misses, orderMap(map[string]string{}))
		}
		added := map[string]string{"x-added": "1"}
		for k, e := range entries {
			added[k] = e
			changed := map[string]string{}
			unknown := map[string]attr.Value{}
			for k2, e2 := range entries {
				changed[k2] = e2
				unknown[k2] = types.StringValue(e2)
			}
			changed[k] = e + " "
			unknown[k] = types.StringUnknown()
			misses = append(misses, orderMap(changed), types.MapValueMust(types.StringType, unknown))
		}
		return append(misses, orderMap(added))
	}
	t.Fatalf("no near misses for a %T; teach nearMisses the type, and onlyAddressOrderChanged the attribute", v)
	return nil
}

// The near misses must each differ from the value, or the test above could
// pass for an attribute the comparison ignores.
func TestNearMissesDiffer(t *testing.T) {
	t.Parallel()

	for _, addr := range orderPrior().Addresses {
		value := reflect.ValueOf(addr)
		for i := range value.NumField() {
			v := value.Field(i).Interface().(attr.Value)
			misses := nearMisses(t, v)
			if len(misses) == 0 {
				t.Errorf("no near misses for %s", v)
			}
			for _, miss := range misses {
				if miss.Equal(v) {
					t.Errorf("the near miss %s equals %s", miss, v)
				}
			}
		}
	}
}
