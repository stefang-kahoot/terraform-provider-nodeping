package client

import (
	"encoding/json"
)

type Contact struct {
	ID         string                    `json:"_id,omitempty"`
	Type       string                    `json:"type,omitempty"`
	CustomerID string                    `json:"customer_id,omitempty"`
	Name       string                    `json:"name,omitempty"`
	CustRole   string                    `json:"custrole,omitempty"`
	Addresses  map[string]ContactAddress `json:"addresses,omitempty"`
}

type ContactAddress struct {
	Address       string            `json:"address,omitempty"`
	Type          string            `json:"type,omitempty"`
	Status        string            `json:"status,omitempty"`
	SuppressUp    bool              `json:"suppressup,omitempty"`
	SuppressDown  bool              `json:"suppressdown,omitempty"`
	SuppressFirst bool              `json:"suppressfirst,omitempty"`
	SuppressDiag  bool              `json:"suppressdiag,omitempty"`
	SuppressAll   bool              `json:"suppressall,omitempty"`
	Mute          json.RawMessage   `json:"mute,omitempty"`
	Action        string            `json:"action,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	QueryStrings  map[string]string `json:"querystrings,omitempty"`
	Data          interface{}       `json:"data,omitempty"`
	Priority      *int              `json:"priority,omitempty"`
}

type NewAddress struct {
	Address       string            `json:"address"`
	Type          string            `json:"type"`
	SuppressUp    bool              `json:"suppressup,omitempty"`
	SuppressDown  bool              `json:"suppressdown,omitempty"`
	SuppressFirst bool              `json:"suppressfirst,omitempty"`
	SuppressDiag  bool              `json:"suppressdiag,omitempty"`
	SuppressAll   bool              `json:"suppressall,omitempty"`
	Mute          interface{}       `json:"mute,omitempty"`
	Action        string            `json:"action,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	QueryStrings  map[string]string `json:"querystrings,omitempty"`
	Data          string            `json:"data,omitempty"`
	Priority      *int              `json:"priority,omitempty"`
}

// ContactGroup groups contact addresses so a check can notify all of them
// through a single notification entry.
type ContactGroup struct {
	ID         string   `json:"_id,omitempty"`
	Type       string   `json:"type,omitempty"`
	CustomerID string   `json:"customer_id,omitempty"`
	Name       string   `json:"name,omitempty"`
	Members    []string `json:"members,omitempty"`
}

type ContactGroupCreateRequest struct {
	Name string `json:"name,omitempty"`
	// Members holds contact *address* IDs, not contact IDs.
	Members []string `json:"members,omitempty"`
}

type ContactGroupUpdateRequest struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	// Members is sent even when empty so that removing every member from a
	// group actually clears it instead of being omitted from the request.
	Members []string `json:"members"`
}

type ContactCreateRequest struct {
	Name     string `json:"name,omitempty"`
	CustRole string `json:"custrole,omitempty"`
	// NewAddresses is left out when empty: NodePing then creates a contact
	// with no address, but refuses an empty list.
	NewAddresses []NewAddress `json:"newaddresses,omitempty"`
}

type ContactUpdateRequest struct {
	Name     string `json:"name,omitempty"`
	CustRole string `json:"custrole,omitempty"`
	// Addresses, when sent, replaces the contact's addresses: NodePing keeps
	// those it lists, under their IDs, and deletes the rest. A non-nil map is
	// sent even when empty, as {}, which with NewAddresses replaces every
	// address. A nil map leaves the key out, and the addresses as they are.
	// omitzero, unlike omitempty, tells the two apart; it also keeps a nil
	// map from going out as null, which NodePing happens to ignore as well.
	Addresses    map[string]ContactAddress `json:"addresses,omitzero"`
	NewAddresses []NewAddress              `json:"newaddresses,omitempty"`
}

type Check struct {
	ID            string                   `json:"_id,omitempty"`
	Rev           string                   `json:"_rev,omitempty"`
	CustomerID    string                   `json:"customer_id,omitempty"`
	Label         string                   `json:"label,omitempty"`
	Type          string                   `json:"type,omitempty"`
	Interval      json.Number              `json:"interval,omitempty"`
	Enabled       string                   `json:"enable,omitempty"`
	Public        bool                     `json:"public,omitempty"`
	Status        string                   `json:"status,omitempty"`
	Modified      int64                    `json:"modified,omitempty"`
	Created       int64                    `json:"created,omitempty"`
	State         int                      `json:"state,omitempty"`
	FirstDown     interface{}              `json:"firstdown,omitempty"`
	Notifications []map[string]interface{} `json:"notifications,omitempty"`
	Parameters    CheckParameters          `json:"parameters,omitempty"`
	Dep           interface{}              `json:"dep,omitempty"`
	Mute          interface{}              `json:"mute,omitempty"`
	Description   string                   `json:"description,omitempty"`
	Queue         interface{}              `json:"queue,omitempty"` // a queue name, or false on a disabled check
	UUID          string                   `json:"uuid,omitempty"`
	RunLocations  interface{}              `json:"runlocations,omitempty"`
	HomeLoc       interface{}              `json:"homeloc,omitempty"`
	AutoDiag      bool                     `json:"autodiag,omitempty"`
	Tags          []string                 `json:"tags,omitempty"`
}

type CheckParameters struct {
	Target         string                `json:"target,omitempty"`
	Threshold      interface{}           `json:"threshold,omitempty"`
	Sens           interface{}           `json:"sens,omitempty"`
	ContentString  string                `json:"contentstring,omitempty"`
	Regex          interface{}           `json:"regex,omitempty"`
	Invert         interface{}           `json:"invert,omitempty"`
	Follow         interface{}           `json:"follow,omitempty"`
	Method         string                `json:"method,omitempty"`
	StatusCode     interface{}           `json:"statuscode,omitempty"`
	SendHeaders    HeaderMap             `json:"sendheaders,omitempty"`
	ReceiveHeaders HeaderMap             `json:"receiveheaders,omitempty"`
	Data           interface{}           `json:"data,omitempty"`
	PostData       string                `json:"postdata,omitempty"`
	Port           interface{}           `json:"port,omitempty"`
	Username       string                `json:"username,omitempty"`
	Password       string                `json:"password,omitempty"`
	Secure         interface{}           `json:"secure,omitempty"`
	Verify         interface{}           `json:"verify,omitempty"`
	IPv6           interface{}           `json:"ipv6,omitempty"`
	DNSType        string                `json:"dnstype,omitempty"`
	DNSToResolve   string                `json:"dnstoresolve,omitempty"`
	DNSSection     string                `json:"dnssection,omitempty"`
	DNSRD          interface{}           `json:"dnsrd,omitempty"`
	Transport      string                `json:"transport,omitempty"`
	WarningDays    interface{}           `json:"warningdays,omitempty"`
	ServerName     string                `json:"servername,omitempty"`
	Email          string                `json:"email,omitempty"`
	Database       string                `json:"database,omitempty"`
	Query          string                `json:"query,omitempty"`
	Namespace      string                `json:"namespace,omitempty"`
	Fields         map[string]CheckField `json:"fields,omitempty"`
	Hosts          map[string]RedisHost  `json:"hosts,omitempty"`
	RedisType      string                `json:"redistype,omitempty"`
	SentinelName   string                `json:"sentinelname,omitempty"`
	SSHKey         interface{}           `json:"sshkey,omitempty"`
	ClientCert     interface{}           `json:"clientcert,omitempty"`
	CheckToken     string                `json:"checktoken,omitempty"`
	OldResultFail  interface{}           `json:"oldresultfail,omitempty"`
	Ignore         string                `json:"ignore,omitempty"`
	DoHDoT         string                `json:"dohdot,omitempty"`
	EDNS           map[string]string     `json:"edns,omitempty"`
	WhoisServer    string                `json:"whoisserver,omitempty"`
	RDAPUrl        string                `json:"rdapurl,omitempty"`
	SNMPv          string                `json:"snmpv,omitempty"`
	SNMPCom        string                `json:"snmpcom,omitempty"`
	VerifyVolume   interface{}           `json:"verifyvolume,omitempty"`
	VolumeMin      interface{}           `json:"volumemin,omitempty"`
}

// HeaderMap is a check's sendheaders or receiveheaders as the API returns
// them. NodePing stores some headers as null -- {"Host": null} -- meaning no
// such header; a plain map[string]string would decode that as "", and the
// check would read back as sending an empty Host header. Null entries are
// dropped instead, so a map holding nothing else reads as no headers at all.
// An empty string is a value and is kept. Requests keep a plain map: only
// reading changes.
type HeaderMap map[string]string

// UnmarshalJSON decodes a JSON object of strings, leaving out the null ones.
func (h *HeaderMap) UnmarshalJSON(data []byte) error {
	var raw map[string]*string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == nil {
		*h = nil
		return nil
	}

	out := make(HeaderMap, len(raw))
	for name, value := range raw {
		if value != nil {
			out[name] = *value
		}
	}
	*h = out
	return nil
}

type CheckField struct {
	Name  string      `json:"name,omitempty"`
	Min   interface{} `json:"min,omitempty"`
	Max   interface{} `json:"max,omitempty"`
	Match string      `json:"match,omitempty"`
}

type RedisHost struct {
	Host     string `json:"host"`
	Port     int    `json:"port,omitempty"`
	Password string `json:"password,omitempty"`
}

// CheckCreateRequest is a check as sent to NodePing, on create and, inside
// CheckUpdateRequest, on update.
//
// NodePing merges an update into the check: whatever it leaves out keeps its
// stored value. Removing a value therefore means sending the one that clears
// it, and for some attributes that is an empty value omitempty would drop.
// Those fields are typed to carry it: a pointer is left out when nil and sent
// when set, "" included; an interface{} is left out only when nil, so it can
// carry false or "". The check resource's clearRemoved says which value
// clears what.
type CheckCreateRequest struct {
	Type           string                   `json:"type"`
	Target         string                   `json:"target,omitempty"`
	Label          string                   `json:"label,omitempty"`
	Interval       interface{}              `json:"interval,omitempty"`
	Enabled        string                   `json:"enabled,omitempty"`
	Public         interface{}              `json:"public,omitempty"`
	AutoDiag       interface{}              `json:"autodiag,omitempty"`
	RunLocations   interface{}              `json:"runlocations,omitempty"` // [] clears them
	HomeLoc        interface{}              `json:"homeloc,omitempty"`
	Threshold      interface{}              `json:"threshold,omitempty"`
	Sens           interface{}              `json:"sens,omitempty"`
	Notifications  []map[string]interface{} `json:"notifications,omitempty"`
	Dep            interface{}              `json:"dep,omitempty"` // a check ID, or false to remove the dependency
	Mute           interface{}              `json:"mute,omitempty"`
	Description    string                   `json:"description,omitempty"` // " " clears it; NodePing ignores "", null, false and 0
	Tags           []string                 `json:"tags,omitempty"`
	ContentString  *string                  `json:"contentstring,omitempty"`  // "" clears it
	Regex          interface{}              `json:"regex,omitempty"`          // false clears it
	Invert         interface{}              `json:"invert,omitempty"`         // false clears it
	Follow         interface{}              `json:"follow,omitempty"`         // false clears it
	Method         *string                  `json:"method,omitempty"`         // "" clears it
	StatusCode     interface{}              `json:"statuscode,omitempty"`     // a number, or "" to clear it
	SendHeaders    map[string]*string       `json:"sendheaders,omitempty"`    // merged per header; a nil value deletes the header
	ReceiveHeaders map[string]*string       `json:"receiveheaders,omitempty"` // merged per header; a nil value deletes the header
	Data           interface{}              `json:"data,omitempty"`
	PostData       *string                  `json:"postdata,omitempty"` // "" clears it
	Port           interface{}              `json:"port,omitempty"`
	Username       string                   `json:"username,omitempty"`
	Password       string                   `json:"password,omitempty"`
	Secure         interface{}              `json:"secure,omitempty"`
	Verify         interface{}              `json:"verify,omitempty"`
	IPv6           interface{}              `json:"ipv6,omitempty"` // false clears it
	DNSType        string                   `json:"dnstype,omitempty"`
	DNSToResolve   string                   `json:"dnstoresolve,omitempty"`
	DNSSection     string                   `json:"dnssection,omitempty"`
	DNSRD          interface{}              `json:"dnsrd,omitempty"`
	Transport      string                   `json:"transport,omitempty"`
	WarningDays    interface{}              `json:"warningdays,omitempty"` // a number, or "" to clear it
	ServerName     *string                  `json:"servername,omitempty"`  // "" clears it
	Email          string                   `json:"email,omitempty"`
	Database       string                   `json:"database,omitempty"`
	Query          string                   `json:"query,omitempty"`
	Namespace      string                   `json:"namespace,omitempty"`
	Fields         map[string]CheckField    `json:"fields,omitempty"`
	Hosts          map[string]RedisHost     `json:"hosts,omitempty"`
	RedisType      string                   `json:"redistype,omitempty"`
	SentinelName   string                   `json:"sentinelname,omitempty"`
	SSHKey         string                   `json:"sshkey,omitempty"`
	ClientCert     string                   `json:"clientcert,omitempty"`
	CheckToken     string                   `json:"checktoken,omitempty"`
	OldResultFail  interface{}              `json:"oldresultfail,omitempty"`
	Ignore         string                   `json:"ignore,omitempty"`
	DoHDoT         string                   `json:"dohdot,omitempty"`
	EDNS           map[string]string        `json:"edns,omitempty"`
	WhoisServer    string                   `json:"whoisserver,omitempty"`
	RDAPUrl        string                   `json:"rdapurl,omitempty"`
	SNMPv          string                   `json:"snmpv,omitempty"`
	SNMPCom        string                   `json:"snmpcom,omitempty"`
	VerifyVolume   interface{}              `json:"verifyvolume,omitempty"`
	VolumeMin      interface{}              `json:"volumemin,omitempty"`
}

type CheckUpdateRequest struct {
	CheckCreateRequest
	// Tags takes the place of the embedded omitempty one: NodePing keeps a
	// check's tags when an update leaves them out, so removing the last tag
	// has to send an empty list. A nil Tags goes out as null, which NodePing
	// ignores as well.
	Tags []string `json:"tags"`
	// Notifications takes the place of the embedded omitempty one for the
	// same reason: NodePing replaces a check's notifications with the list an
	// update sends, keeps them when the update leaves it out, and ignores
	// null, so removing the last notification has to send an empty list.
	Notifications []map[string]interface{} `json:"notifications"`
}

type Notification struct {
	Delay    int    `json:"delay"`
	Schedule string `json:"schedule"`
}

type DeleteResponse struct {
	OK bool   `json:"ok"`
	ID string `json:"id"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
