package traceql

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang/protobuf/jsonpb" //nolint:all
	"github.com/golang/protobuf/proto"  //nolint:all
	"github.com/grafana/dskit/user"
	"github.com/grafana/tempo/pkg/tempopb"
)

// PerTenantMetaDataStore: this makes a big assumption that the metadata is updated and correct.
// we might need to make sure this assumption holds by regularly updating the metadata
// or evicting the metadata from the cache if it's too old.
var PerTenantMetaDataStore = NewMetaDataStore(5 * time.Second)

type MetaData struct {
	tenantID     string
	SpanTags     map[string]string
	ResourceTags map[string]string
}

type MetaDataStore struct {
	metadata map[string]*MetaData

	mtx sync.Mutex

	refreshInterval time.Duration
}

func NewMetaDataStore(refreshInterval time.Duration) *MetaDataStore {
	store := &MetaDataStore{
		metadata:        make(map[string]*MetaData),
		refreshInterval: refreshInterval,
	}
	// refresh the metadata store at regular intervals
	go store.RefreshMetaDataStore(context.Background())

	return store
}

func (m *MetaDataStore) GetMetaData(tenantID string) (*MetaData, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if md, ok := m.metadata[tenantID]; ok {
		return md, nil
	}

	fmt.Printf("GetMetaData: fetch metadata for the tenantID: %s\n", tenantID)

	// fetch and add metadata to store if it doesn't exist
	md, err := fetchMetaData(tenantID)
	if err != nil {
		fmt.Printf("failed to fetch metadata for tenantID: %s, err: %s\n", tenantID, err.Error())
		return nil, err
	}

	m.metadata[tenantID] = md
	return md, nil
}

// RefreshMetaDataStore refreshes the metadata store at regular intervals
// TODO: this is in mem for now, but if we put it in prod, it should be backed by a database???
func (store *MetaDataStore) RefreshMetaDataStore(ctx context.Context) {
	ticker := time.NewTicker(store.refreshInterval)
	defer ticker.Stop()

	fetchAll := func() {
		store.mtx.Lock() // TODO: this lock is bad, maybe we should use a RWMutex
		for tenantID, _ := range store.metadata {
			// Refresh metadata for each tenant
			fmt.Printf("Refreshing metadata for tenantID: %s\n", tenantID)
			newMd, err := fetchMetaData(tenantID)
			if err != nil {
				fmt.Printf("failed to fetch metadata for tenantID: %s, err: %s\n", tenantID, err.Error())
				continue
			}

			fmt.Printf("Refreshed metadata for tenantID: %s\n", tenantID)
			store.metadata[tenantID] = newMd
		}
		store.mtx.Unlock()

	}

	// run a ticker for regular refresh
	for {
		select {
		case <-ticker.C:
			fetchAll()
		case <-ctx.Done():
			return
		}
	}
}

// FIXME: this is now making a query to the Tempo API, which is not good
// we can just use the internal API to get the metadata, instead of making an HTTP API Call
func fetchMetaData(tenantID string) (*MetaData, error) {
	fmt.Printf("optimizeConditions: tenantID: %s\n", tenantID)

	md := &MetaData{
		tenantID: tenantID,
	}

	// FIXME: these API calls are basically DDoSIng us with the tags and tag values endpoints??
	// maybe we should run this in a timer and cache the results per tenant in the memcached or in a database??

	client := NewClient("http://localhost:3200", tenantID)
	// results of  api/v2/search/tags?scope=span
	spanTagsRsp, err := client.SearchTagsV2WithScope("span")
	if err != nil {
		// if we can't get the span tags we can't optimize the query
		fmt.Printf("optimizeConditions: failed to get span tags, err: %s\n", err.Error())
		return nil, err
	}

	spanTags := make(map[string]string)
	for _, scope := range spanTagsRsp.Scopes {
		// fmt.Printf("optimizeConditions: scope: %s\n", scope)
		if scope.Name == "span" {
			for _, t := range scope.Tags {
				spanTags[t] = ""
			}
		}
	}
	md.SpanTags = spanTags

	// results of v2/search/tags?scope=resource
	resourceTagsRsp, err2 := client.SearchTagsV2WithScope("resource")
	if err2 != nil {
		// if we can't get the resource tags we can't optimize the query
		fmt.Printf("optimizeConditions: failed to get resource tags, err: %s\n", err.Error())
		return nil, err2
	}
	resourceTags := make(map[string]string)
	for _, scope := range resourceTagsRsp.Scopes {
		// fmt.Printf("optimizeConditions: scope: %s\n", scope)
		if scope.Name == "resource" {
			for _, t := range scope.Tags {
				resourceTags[t] = ""
			}
		}
	}
	md.ResourceTags = resourceTags

	return md, nil
}

// optimizeConditions will optimize the un-scoped conditions to scoped conditions if the attribute
// is only found on span or resource attributes in the metadata.
func optimizeConditions(ctx context.Context, spanReq *FetchSpansRequest) *FetchSpansRequest {
	tenantID, err1 := user.ExtractOrgID(ctx)
	if err1 != nil {
		// if we can't get the tenantID, we can't optimize the query
		fmt.Printf("optimizeConditions: failed to extract tenantID, err: %s\n", err1.Error())
		return spanReq
	}

	md, err := PerTenantMetaDataStore.GetMetaData(tenantID)
	if err != nil {
		fmt.Printf("optimizeConditions: failed to get metadata, err: %s\n", err.Error())
		return spanReq
	}

	for i, cond := range spanReq.Conditions {
		switch cond.Attribute.Scope {
		case AttributeScopeNone:
			// skip Intrinsics
			if !IsIntrinsic(cond.Attribute.Intrinsic) {
				// now we can lookup and modify the request to go from un-scoped to scoped

				_, foundInSpanTags := md.SpanTags[cond.Attribute.Name]
				_, foundInResourceTags := md.ResourceTags[cond.Attribute.Name]

				if foundInSpanTags && foundInResourceTags {
					// tag is in both span and resource tags, so we can't optimize it
					// move to next condition
					fmt.Printf("optimizeConditions: found in both span and resource tags: %s\n", cond.Attribute.Name)
					break
				}

				if foundInResourceTags {
					fmt.Printf("optimizeConditions: found in resource tags, rewrote scope for attr: %s\n", cond.Attribute.Name)
					cond.Attribute.Scope = AttributeScopeResource
					spanReq.Conditions[i] = cond
				} else if foundInSpanTags {
					fmt.Printf("optimizeConditions: found in span tags, rewrote scope for attr: %s\n", cond.Attribute.Name)
					cond.Attribute.Scope = AttributeScopeSpan
					spanReq.Conditions[i] = cond
				}
			}
			// this is the core code that handles un-scoped conditions,
		default:
			// no op, don't touch other scopes
		}
	}
	return spanReq
}

// ----- mostly copied and pasted code, for hackathon -----

// this is copy, but good enough for hackathon
var intrinsicColumnLookups = map[Intrinsic]struct {
	scope AttributeScope
}{
	IntrinsicName:                 {},
	IntrinsicStatus:               {},
	IntrinsicStatusMessage:        {},
	IntrinsicDuration:             {},
	IntrinsicKind:                 {},
	IntrinsicSpanID:               {},
	IntrinsicSpanStartTime:        {},
	IntrinsicStructuralDescendant: {}, // Not a real column, this entry is only used to assign default scope.
	IntrinsicStructuralChild:      {}, // Not a real column, this entry is only used to assign default scope.
	IntrinsicStructuralSibling:    {}, // Not a real column, this entry is only used to assign default scope.
	IntrinsicNestedSetLeft:        {},
	IntrinsicNestedSetRight:       {},
	IntrinsicNestedSetParent:      {},

	IntrinsicTraceRootService: {},
	IntrinsicTraceRootSpan:    {},
	IntrinsicTraceDuration:    {},
	IntrinsicTraceID:          {},
	IntrinsicTraceStartTime:   {},

	IntrinsicEventName:           {},
	IntrinsicEventTimeSinceStart: {},
	IntrinsicLinkTraceID:         {},
	IntrinsicLinkSpanID:          {},

	IntrinsicInstrumentationName:    {},
	IntrinsicInstrumentationVersion: {},

	IntrinsicServiceStats: {}, // Not a real column, this entry is only used to assign default scope.
}

func IsIntrinsic(name Intrinsic) bool {
	if name == IntrinsicNone {
		return false
	}

	if _, ok := intrinsicColumnLookups[name]; ok {
		return true
	}

	return false
}

const (
	orgIDHeader = "X-Scope-OrgID"

	QueryTraceEndpoint   = "/api/traces"
	QueryTraceV2Endpoint = "/api/v2/traces"

	acceptHeader        = "Accept"
	applicationProtobuf = "application/protobuf"
	applicationJSON     = "application/json"
)

// Client is client to the Tempo API.
type Client struct {
	BaseURL string
	OrgID   string
	client  *http.Client
	headers map[string]string
}

func NewClient(baseURL, orgID string) *Client {
	return &Client{
		BaseURL: baseURL,
		OrgID:   orgID,
		client:  http.DefaultClient,
	}
}

func (c *Client) SearchTagsV2WithScope(scope string) (*tempopb.SearchTagsV2Response, error) {
	m := &tempopb.SearchTagsV2Response{}
	_, err := c.getFor(c.BaseURL+"/api/v2/search/tags?scope="+scope, m)
	if err != nil {
		return nil, err
	}

	return m, nil
}

// getFor sends a GET request and attempts to unmarshal the response.
func (c *Client) getFor(url string, m proto.Message) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	marshallingFormat := applicationJSON
	if strings.Contains(url, QueryTraceEndpoint) || strings.Contains(url, QueryTraceV2Endpoint) {
		marshallingFormat = applicationProtobuf
	}
	// Set 'Accept' header to 'application/protobuf'.
	// This is required for the /api/traces and /api/v2/traces endpoint to return a protobuf response.
	// JSON lost backwards compatibility with the upgrade to `opentelemetry-proto` v0.18.0.
	req.Header.Set(acceptHeader, marshallingFormat)

	resp, body, err := c.doRequest(req)
	if err != nil {
		return nil, err
	}

	switch marshallingFormat {
	case applicationJSON:
		if err = jsonpb.UnmarshalString(string(body), m); err != nil {
			return resp, fmt.Errorf("error decoding %T json, err: %v body: %s", m, err, string(body))
		}
	default:
		if err = proto.Unmarshal(body, m); err != nil {
			return nil, fmt.Errorf("error decoding %T proto, err: %w body: %s", m, err, string(body))
		}
	}

	return resp, nil
}

// doRequest sends the given request, it injects X-Scope-OrgID and handles bad status codes.
func (c *Client) doRequest(req *http.Request) (*http.Response, []byte, error) {
	if len(c.OrgID) > 0 {
		req.Header.Set(orgIDHeader, c.OrgID)
	}

	if c.headers != nil {
		for k, v := range c.headers {
			req.Header.Set(k, v)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("error querying Tempo %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 400 && resp.StatusCode <= 599 {
		body, _ := io.ReadAll(resp.Body)
		return resp, body, fmt.Errorf("%s request to %s failed with response: %d body: %s", req.Method, req.URL.String(), resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("error reading response body: %w", err)
	}

	return resp, body, nil
}
