package sfm

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cfn "github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cfntyp "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/google/uuid"
	"gopkg.in/yaml.v2"
)

var defaultCaps = []cfntyp.Capability{cfntyp.CapabilityCapabilityNamedIam, cfntyp.CapabilityCapabilityAutoExpand}

// Handle is a wrapper for service clients. Use it to get, list, delete stacks
// by name.
type Handle struct {
	CFNcli *cfn.Client
}

// Stack is a wrapper for the cloudformation stack struct with simplified
// fields.
type Stack struct {
	NoRollback bool
	TermProc   bool

	Name   string
	Short  string // ok, prog, err
	Status string
	Reason string
	Desc   string

	Caps    []string
	Topics  []string
	Params  map[string]string
	Outputs map[string]string
	Tags    map[string]string

	Created time.Time
	Updated time.Time

	Handle       Handle `json:"-" yaml:"-"`
	Template     Template
	TemplateBody string `json:"-" yaml:"-"`
}

// Template contains the content of the cloudformation template and probably
// some helper functions.
type Template struct {
	AWSTemplateFormatVersion string                 `yaml:"AWSTemplateFormatVersion,omitempty"`
	Transform                string                 `yaml:"Transform,omitempty"`
	Description              string                 `yaml:"Description,omitempty"`
	Metadata                 map[string]interface{} `yaml:"Metadata,omitempty"`
	Parameters               map[string]interface{} `yaml:"Parameters,omitempty"`
	Mappings                 map[string]interface{} `yaml:"Mappings,omitempty"`
	Conditions               map[string]interface{} `yaml:"Conditions,omitempty"`
	Resources                map[string]interface{} `yaml:"Resources,omitempty"`
	Outputs                  map[string]interface{} `yaml:"Outputs,omitempty"`
}

// Event is a cloudformation event.
type Event struct {
	ID        string
	Resource  string
	Status    string
	Reason    string
	Timestamp time.Time
	Token     string
}

// NewHandle returns a new Handle with service clients created from the
// supplied AWS config struct.
func NewHandle(cfg aws.Config) (Handle, error) {
	h := Handle{CFNcli: cfn.NewFromConfig(cfg)}
	return h, nil
}

// NewStack returns a Stack which may be pre-populated with values if it
// already exists.
func (h Handle) NewStack(name string) Stack {
	s, err := h.Get(name)
	if err != nil {
		return Stack{Name: name, Handle: h}
	}
	s.Handle = h
	return s
}

// List returns a slice of Stack structs and an error. The supplied glob
// filters stacks based on the stack name.
func (h Handle) List(glob string) ([]Stack, error) {
	if glob == "" {
		glob = "*"
	}

	ss := []Stack{}
	pg := cfn.NewDescribeStacksPaginator(h.CFNcli, &cfn.DescribeStacksInput{})
	i := 0
	for pg.HasMorePages() && i < 200 {
		i++
		o, err := pg.NextPage(context.Background())
		if err != nil {
			return ss, fmt.Errorf("cant page: %w", err)
		}
		for _, cs := range o.Stacks {
			n := *cs.StackName
			if glob != "*" {
				if m, _ := filepath.Match(glob, n); !m {
					continue
				}
			}
			ss = append(ss, NewFromAWS(cs))
		}
	}

	return ss, nil
}

// Get returns a single Stack and an error.
func (h Handle) Get(name string) (Stack, error) {
	o, err := h.CFNcli.DescribeStacks(
		context.Background(),
		&cfn.DescribeStacksInput{StackName: aws.String(name)},
	)
	if err != nil {
		return Stack{}, fmt.Errorf("cant describe stack: %w", err)
	}
	if len(o.Stacks) < 1 {
		return Stack{}, fmt.Errorf("stack '%s' not found", name)
	}

	s := NewFromAWS(o.Stacks[0])
	s.Handle = h
	return s, nil
}

// Make creates or updates a stack and returns a ClientRequestToken and an error.
func (h Handle) Make(s Stack) (string, error) {
	if s.Name == "" {
		return "", errors.New("missing stack name")
	}
	if len(s.TemplateBody) < 1 {
		return "", errors.New("stack has empty template")
	}
	token := uuid.NewString()
	i := &cfn.CreateStackInput{
		StackName:          aws.String(s.Name),
		DisableRollback:    aws.Bool(s.NoRollback),
		Capabilities:       defaultCaps,
		Parameters:         s.paramsToAWS(),
		Tags:               s.tagsToAWS(),
		TemplateBody:       aws.String(s.TemplateBody),
		NotificationARNs:   s.Topics,
		ClientRequestToken: &token,
	}

	_, err := h.CFNcli.CreateStack(context.Background(), i)
	if err != nil {
		var aee *cfntyp.AlreadyExistsException
		if errors.As(err, &aee) {
			return h.update(s)
		}
		return token, fmt.Errorf("cant create stack: %w", err)
	}

	return token, nil
}

// Delete deletes a stack and returns a ClientRequestToken and an error.
func (h Handle) Delete(name string) (string, error) {
	token := uuid.NewString()
	_, err := h.CFNcli.DeleteStack(
		context.Background(),
		&cfn.DeleteStackInput{
			StackName:          aws.String(name),
			ClientRequestToken: &token,
		},
	)
	if err != nil {
		err = fmt.Errorf("cant delete stack: %w", err)
	}
	return token, err
}

func (h Handle) update(s Stack) (string, error) {
	token := uuid.NewString()
	i := &cfn.UpdateStackInput{
		StackName:          aws.String(s.Name),
		Capabilities:       defaultCaps,
		Parameters:         s.paramsToAWS(),
		Tags:               s.tagsToAWS(),
		TemplateBody:       aws.String(s.TemplateBody),
		NotificationARNs:   s.Topics,
		ClientRequestToken: &token,
	}

	_, err := h.CFNcli.UpdateStack(context.Background(), i)
	if err != nil {
		if strings.HasSuffix(err.Error(), "No updates are to be performed.") {
			return token, nil
		}
		return token, fmt.Errorf("cant update stack: %w", err)
	}

	return token, nil
}

// Resources returns up to 100 resources for the supplied Stack receiver.
func (s Stack) Resources() (map[string]map[string]string, error) {
	if s.Handle.CFNcli == nil {
		return nil, errors.New("Stack has no Handle")
	}
	i := &cfn.DescribeStackResourcesInput{StackName: aws.String(s.Name)}
	o, err := s.Handle.CFNcli.DescribeStackResources(context.Background(), i)
	if err != nil {
		return nil, fmt.Errorf("cant describe stack resources: %w", err)
	}

	mm := map[string]map[string]string{}
	for _, r := range o.StackResources {
		id := *r.LogicalResourceId
		reason := ""
		if r.ResourceStatusReason != nil {
			reason = *r.ResourceStatusReason
		}
		mm[id] = map[string]string{}
		mm[id]["status"] = string(r.ResourceStatus)
		mm[id]["type"] = *r.ResourceType
		mm[id]["updated"] = fmt.Sprintf("%v", *r.Timestamp)
		mm[id]["pid"] = *r.PhysicalResourceId
		mm[id]["reason"] = reason
		mm[id]["stackid"] = *r.StackId
	}
	return mm, nil
}

// Events returns stack events which were generated after the supplied EventId for the supplied request token.
// If no EventId is supplied (an empty string) the most recent Event is returned.
// If no ClientRequestToken is supplied (an empty string) events aren't filtered by request token.
func (s Stack) Events(id string, token string) ([]Event, error) {
	if s.Handle.CFNcli == nil {
		return []Event{}, errors.New("Stack has no Handle")
	}
	i := &cfn.DescribeStackEventsInput{StackName: aws.String(s.Name)}
	o, err := s.Handle.CFNcli.DescribeStackEvents(context.Background(), i)
	if err != nil {
		return nil, fmt.Errorf("cant describe stack events: %w", err)
	}

	events := []Event{}
	for _, e := range o.StackEvents {
		ev := Event{ID: string(*e.EventId), Status: string(e.ResourceStatus), Timestamp: *e.Timestamp, Token: *e.ClientRequestToken}

		if ev.ID == id {
			break
		}

		if ev.Token != token && token != "" {
			continue
		}

		if e.LogicalResourceId != nil {
			ev.Resource = *e.LogicalResourceId
		}
		if e.ResourceStatusReason != nil {
			ev.Reason = *e.ResourceStatusReason
		}

		if id == "" {
			events = []Event{ev}
			break
		}

		events = append([]Event{ev}, events...)
	}

	return events, nil
}

// Pretty returns a string of the event containing colour escape codes ready for printing in a terminal.
func (e Event) Pretty() string {
	lri := "-"
	if e.Resource != "" {
		lri = e.Resource
	}
	if len(lri) > 30 {
		lri = lri[0:27] + "..."
	}

	rs := e.Status
	rsColor := ""
	switch {
	case strings.HasSuffix(string(rs), "_COMPLETE"):
		rsColor = cGreen
	case strings.HasSuffix(string(rs), "_FAILED"):
		rsColor = cRed
	case rs == "UPDATE_ROLLBACK_COMPLETE", rs == "ROLLBACK_COMPLETE":
		rsColor = cCyan
	}
	if len(rs) > 20 {
		rs = rs[0:17] + "..."
	}

	rsr := "-"
	if e.Reason != "" {
		rsr = e.Reason
	}

	loc, _ := time.LoadLocation("Local") // WARN this might break on non-UNIX systems
	tsf := e.Timestamp.In(loc).Format("15:04:05 MST")

	return fmt.Sprintf("%s %-30s %s%-20s%s %s\n", tsf, lri, rsColor, rs, cReset, rsr)
}

func (s Stack) String() string {
	return s.Name
}

// StringVerbose is a stringer which returns more than just a name.
func (s Stack) StringVerbose() string {
	t := s.Created
	if !s.Updated.IsZero() {
		t = s.Updated
	}
	loc, _ := time.LoadLocation("Local")
	return fmt.Sprintf("%s\t%s\t%s", t.In(loc).Format("15:04:05 MST"), s.Name, s.Status)
}

func (s Stack) paramsToAWS() []cfntyp.Parameter {
	pp := []cfntyp.Parameter{}
	for k, v := range s.Params {
		if _, ok := s.Template.Parameters[k]; ok {
			pp = append(pp, cfntyp.Parameter{ParameterKey: aws.String(k), ParameterValue: aws.String(v)})
		}
	}
	return pp
}

func (s Stack) tagsToAWS() []cfntyp.Tag {
	tags := []cfntyp.Tag{}
	for k, v := range s.Tags {
		tags = append(tags, cfntyp.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	return tags
}

func (s *Stack) NewTemplate(body []byte) error {
	if err := yaml.Unmarshal(body, &s.Template); err != nil {
		return fmt.Errorf("cant unmarshal template into stack: %v", err)
	}
	s.TemplateBody = string(body)
	return nil
}

// NewFromAWS converts a cloudformation stack into an sfm Stack.
func NewFromAWS(cs cfntyp.Stack) Stack {
	s := Stack{
		Name:       *cs.StackName,
		Created:    *cs.CreationTime,
		Short:      getShortStatus(cs.StackStatus),
		Status:     string(cs.StackStatus),
		Reason:     str(cs.StackStatusReason),
		Desc:       str(cs.Description),
		NoRollback: *cs.DisableRollback,
		Topics:     cs.NotificationARNs,
		Tags:       map[string]string{},
		Params:     map[string]string{},
		Outputs:    map[string]string{},
	}

	if cs.EnableTerminationProtection != nil {
		s.TermProc = *cs.EnableTerminationProtection
	}

	if cs.LastUpdatedTime != nil {
		s.Updated = *cs.LastUpdatedTime
	}

	for _, c := range cs.Capabilities {
		s.Caps = append(s.Caps, string(c))
	}

	for _, t := range cs.Tags {
		s.Tags[str(t.Key)] = str(t.Value)
	}

	for _, p := range cs.Parameters {
		s.Params[str(p.ParameterKey)] = str(p.ParameterValue)
	}

	for _, o := range cs.Outputs {
		s.Outputs[str(o.OutputKey)] = str(o.OutputValue)
	}

	return s
}

func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func getShortStatus(s cfntyp.StackStatus) string {
	switch s {
	case cfntyp.StackStatusCreateComplete,
		cfntyp.StackStatusImportComplete,
		cfntyp.StackStatusDeleteComplete,
		cfntyp.StackStatusUpdateComplete:
		return "ok"
	case cfntyp.StackStatusCreateInProgress,
		cfntyp.StackStatusDeleteInProgress,
		cfntyp.StackStatusImportInProgress,
		cfntyp.StackStatusImportRollbackInProgress,
		cfntyp.StackStatusReviewInProgress,
		cfntyp.StackStatusRollbackInProgress,
		cfntyp.StackStatusUpdateCompleteCleanupInProgress,
		cfntyp.StackStatusUpdateInProgress,
		cfntyp.StackStatusUpdateRollbackCompleteCleanupInProgress,
		cfntyp.StackStatusUpdateRollbackInProgress:
		return "prog"
	}
	return "err"
}

var cReset = "\033[0m"
var cRed = "\033[31m"
var cGreen = "\033[32m"
var cCyan = "\033[36m"

// var cYellow = "\033[33m"
// var cBlue = "\033[34m"
// var cPurple = "\033[35m"
// var cGray = "\033[37m"
// var cWhite = "\033[97m"
