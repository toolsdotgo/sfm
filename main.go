package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/toolsdotgo/sfm/pkg/sfm"
	"gopkg.in/yaml.v2"
)

var DEBUG = false // set to true via envar
var version = "edge"

type multiFlag []string

func (m *multiFlag) String() string {
	return fmt.Sprintf("%+v", *m)
}
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

type stack struct {
	cfg aws.Config
	cli *cloudformation.Client
}

// Template ...
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

func main() {
	if os.Getenv("DEBUG") != "" {
		DEBUG = true
	}
	// sfm [-h|-v]
	fhelp := flag.Bool("h", false, "show help")
	fver := flag.Bool("v", false, "show version")
	freg := flag.String("r", "", "set aws region")

	flag.Parse()

	// sfm ls [-h|-v] [glob]
	fsList := flag.NewFlagSet("ls", flag.ExitOnError)
	fListHelp := fsList.Bool("h", false, "show help for ls")
	fListVerbose := fsList.Bool("v", false, "list mode verbose")

	// sfm mk [-h] [-p k=v,k=v,k=v...] [-t template] [-norb] <stack>
	var pff multiFlag
	fsMake := flag.NewFlagSet("mk", flag.ExitOnError)
	fsMake.Var(&pff, "pf", "params file as yaml or json")
	fMakeHelp := fsMake.Bool("h", false, "show help for mk")
	fMakeParams := fsMake.String("p", "", "k=v,k=v... parameters for the template")
	fMakeTempl := fsMake.String("t", "", "template file - or pass one in on stdin")
	fMakeSNS := fsMake.String("sns", "", "sns arns to notify")
	fMakeNoRB := fsMake.Bool("norb", false, "do not rollback on error")
	fMakeWait := fsMake.String("wait", "", "block on the operation, value is: dots, events, ???")
	fMakeTags := fsMake.String("tags", "", "k=v,k=v... tags for the stack")
	fMakeTagsFile := fsMake.String("tagsfile", "", "yaml of json file containing tags for the stack")

	// sfm rm [-h] <stack>
	fsRemv := flag.NewFlagSet("rm", flag.ExitOnError)
	fRemvHelp := fsRemv.Bool("h", false, "show help for rm")
	fRemvForce := fsRemv.Bool("force", false, "try to automagically remove buckets - DATA LOSS")
	fRemvWait := fsRemv.String("wait", "", "block on the operation, value is: dots, events, ???")

	// sfm wait [-h] <stack>
	fsWait := flag.NewFlagSet("wait", flag.ExitOnError)
	fWaitHelp := fsWait.Bool("h", false, "show help for wait")
	fWaitDots := fsWait.Bool("dots", false, "show progress with dots")
	fWaitEvents := fsWait.Bool("events", false, "print events as they are polled")

	// sfm stat [-h] <stack>
	fsStat := flag.NewFlagSet("stat", flag.ExitOnError)
	fStatHelp := fsStat.Bool("h", false, "show help for stat")
	fStatOutputs := fsStat.Bool("o", false, "output stack outputs")
	fStatParams := fsStat.Bool("p", false, "output stack parameters")
	fStatTags := fsStat.Bool("t", false, "output stack tags")
	fStatRes := fsStat.Bool("r", false, "output stack resources")
	fStatEncoding := fsStat.String("e", "text", "output encoding: text, yaml, json")

	if *fver {
		fmt.Println(version)
		os.Exit(0)
	}
	if flag.NArg() == 0 || *fhelp {
		fmt.Print(usageTop)
		os.Exit(64)
	}
	region := *freg
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		region = os.Getenv("AWS_DEFAULT_REGION")
	}
	if region == "" {
		fmt.Fprintln(os.Stderr, "no region set - as flag or envar")
		os.Exit(1)
	}

	switch flag.Arg(0) {
	case "ls":
		_ = fsList.Parse(flag.Args()[1:])
	case "mk":
		_ = fsMake.Parse(flag.Args()[1:])
	case "rm":
		_ = fsRemv.Parse(flag.Args()[1:])
	case "wait":
		_ = fsWait.Parse(flag.Args()[1:])
	case "stat":
		_ = fsStat.Parse(flag.Args()[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand '%s'\n", flag.Arg(0))
		fmt.Print(usageTop)
		os.Exit(64)
	}

	cfg, err := config.LoadDefaultConfig(
		context.TODO(),
		config.WithRegion(region),
		config.WithRetryer(func() aws.Retryer {
			retryer := retry.AddWithMaxAttempts(retry.NewStandard(), 10)
			return retry.AddWithMaxBackoffDelay(retryer, 30*time.Second)
		}),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cant get aws config: %v\n", err)
		os.Exit(1)
	}
	s := stack{cfg: cfg, cli: cloudformation.NewFromConfig(cfg)}

	if fsList.Parsed() {
		if *fListHelp {
			fmt.Print(usageList)
			os.Exit(64)
		}
		os.Exit(s.list(fsList.Args(), *fListVerbose))
	}
	if fsMake.Parsed() {
		if *fMakeHelp {
			fmt.Print(usageMake)
			os.Exit(64)
		}
		os.Exit(s.make(fsMake.Args(), *fMakeTempl, *fMakeParams, pff, *fMakeNoRB, *fMakeWait, *fMakeTags, *fMakeTagsFile, *fMakeSNS))
	}
	if fsRemv.Parsed() {
		if *fRemvHelp {
			fmt.Print(usageRemv)
			os.Exit(64)
		}
		os.Exit(s.remv(fsRemv.Args(), *fRemvForce, *fRemvWait))
	}
	if fsWait.Parsed() {
		if *fWaitHelp {
			fmt.Print(usageWait)
			os.Exit(64)
		}
		os.Exit(s.wait(fsWait.Args(), *fWaitDots, *fWaitEvents))
	}
	if fsStat.Parsed() {
		if *fStatHelp {
			fmt.Print(usageStat)
			os.Exit(64)
		}
		os.Exit(s.stat(fsStat.Args(), *fStatOutputs, *fStatParams, *fStatTags, *fStatRes, *fStatEncoding))
	}
}

func (s stack) list(args []string, verbose bool) int {
	if len(args) > 1 {
		fmt.Fprintln(os.Stderr, "ls accepts one positional argument, a glob")
		fmt.Print(usageList)
		return 64
	}
	glob := "*"
	if len(args) > 0 {
		glob = args[0]
	}

	h := sfm.Handle{CFNcli: s.cli}
	ss, err := h.List(glob)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cant list stacks: %v\n", err)
		return 1
	}
	for _, s := range ss {
		if verbose {
			fmt.Println(s.StringVerbose())
			continue
		}
		fmt.Println(s)
	}

	return 0
}

func (s stack) make(args []string, tmpl string, params string, pFiles []string, norb bool, wait, tags, tagsFile string, sns string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "mk accepts one positional argument, the name of the stack")
		fmt.Print(usageMake)
		return 64
	}
	stack := args[0]
	inPipe := havePipe()

	if tmpl == "" && !inPipe {
		fmt.Fprintln(os.Stderr, "no template flag supplied and no pipe on stdin")
		fmt.Print(usageMake)
		return 64
	}

	var err error
	var r io.Reader
	r = os.Stdin
	if tmpl != "" {
		if strings.HasPrefix(tmpl, "s3://") {
			r, err = openS3(s.cfg, tmpl)
		} else {
			r, err = os.Open(path.Clean(tmpl))
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "cant open template '%s': %v\n", tmpl, err)
			return 1
		}
		if inPipe {
			fmt.Fprintln(os.Stderr, "WARN using template file; ignoring stdin")
		}
	}

	b, err := io.ReadAll(r)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cant read template: %v\n", err)
		return 2
	}

	// create parameter map and cloudformantion parameter slice
	cfpp := []types.Parameter{}
	pmap := map[string]string{}

	// load param files in order
	for _, f := range pFiles {
		pp, err := loadYamlFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cant load params file '%s': %v\n", f, err)
			return 66
		}
		for k, v := range pp {
			pmap[k] = v
		}
	}
	for _, kvp := range strings.Split(params, ",") {
		if kvp == "" {
			continue
		}
		els := strings.SplitN(kvp, "=", 2)
		if len(els) != 2 {
			fmt.Fprintf(os.Stderr, "param kvp '%v' missing '=' splitter, ignoring\n", kvp)
			continue
		}
		pmap[els[0]] = els[1]
	}

	if DEBUG {
		msg := ""
		for k, v := range pmap {
			msg += fmt.Sprintf("%s=%s\n", k, v)
		}
		fmt.Fprintf(os.Stderr, "DEBUG params:\n%s\n", msg)
	}

	// load the template
	var cftpl Template
	err = yaml.Unmarshal(b, &cftpl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "can't unmarshal template: %v\n", err)
		return 66
	}

	// only use params that are required by the template
	for k, v := range pmap {
		if _, ok := cftpl.Parameters[k]; ok {
			cfpp = append(cfpp, types.Parameter{ParameterKey: aws.String(k), ParameterValue: aws.String(v)})
		}
	}

	// create tag map and cloudformation tag slice
	tagpp := []types.Tag{}
	tagmap, err := loadYamlFile(tagsFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cant load tags file: %v\n", err)
		return 66
	}
	for _, kvp := range strings.Split(tags, ",") {
		if kvp == "" {
			continue
		}
		els := strings.SplitN(kvp, "=", 2)
		if len(els) != 2 {
			fmt.Fprintf(os.Stderr, "tag kvp '%v' missing '=' splitter, ignoring\n", kvp)
			continue
		}
		tagmap[els[0]] = els[1]
	}
	for k, v := range tagmap {
		tagpp = append(tagpp, types.Tag{Key: aws.String(k), Value: aws.String(v)})
	}

	outPipe := isPiped() // if the output is being piped, print the stack name

	dots := wait == "dots"
	events := wait == "events"

	// check if stack already exists and do an update if it does
	// the preference would be to create the stack and then update only
	// if the stack operation returns AlreadyExistsException, but the guard
	// below doesn't work right now ~ retest post 1.0.0 i guess?
	o, err := s.cli.DescribeStacks(
		context.TODO(),
		&cloudformation.DescribeStacksInput{StackName: aws.String(stack)},
	)
	dserr := err

	arns := []string{}
	if sns != "" {
		arns = strings.Split(sns, ",")
	}

	var createFailed bool
	if err == nil {
		createFailed = (o.Stacks[0].StackStatus == types.StackStatusCreateFailed || // stack failed to create
			o.Stacks[0].StackStatus == types.StackStatusRollbackFailed || // stack failed to create and failed to rollback creation
			o.Stacks[0].StackStatus == types.StackStatusRollbackComplete) // stack failed to create but successfully rolled back
	}
	if err == nil && !createFailed {
		// check the existing parameters against the supplied parameters and fill in the blanks
		for _, p := range o.Stacks[0].Parameters {
			if _, ok := pmap[*p.ParameterKey]; ok {
				continue
			}
			// only use the params still required by the template
			if _, ok := cftpl.Parameters[*p.ParameterKey]; ok {
				cfpp = append(cfpp, types.Parameter{ParameterKey: p.ParameterKey, UsePreviousValue: aws.Bool(true)})
			}
		}

		pp := &cloudformation.UpdateStackInput{
			StackName:    aws.String(stack),
			Capabilities: []types.Capability{types.CapabilityCapabilityNamedIam, types.CapabilityCapabilityAutoExpand}, // NOTE
			Parameters:   cfpp,
			Tags:         tagpp,
			TemplateBody: aws.String(string(b)),
		}
		if len(arns) > 0 {
			pp.NotificationARNs = arns
		}
		_, err = s.cli.UpdateStack(context.TODO(), pp)
		if err != nil {
			// WARN this is heaps dirty and i hate it
			if strings.HasSuffix(err.Error(), "No updates are to be performed.") {
				fmt.Fprintln(os.Stderr, "no update required")
				if outPipe {
					fmt.Println(stack)
				}
				return 0
			}
			fmt.Fprintf(os.Stderr, "cant update stack '%s': %v\n", stack, err)
			return 3
		}
		if dots || events {
			err := s.block(stack, dots, events)
			fmt.Println() // HAHA YUCKY
			if err != nil {
				fmt.Fprintf(os.Stderr, "error on wait: %v\n", err)
				return 1
			}
		}
		if outPipe {
			fmt.Println(stack)
		}
		return 0
	}

	if createFailed {
		pp := &cloudformation.DeleteStackInput{StackName: aws.String(stack)}
		_, err := s.cli.DeleteStack(context.TODO(), pp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "stack is in CREATE_FAILED state and cant delete: %v\n", err)
			return 4
		}
		if err := s.block(stack, false, false); err != nil {
			fmt.Fprintf(os.Stderr, "stack is in CREATE_FAILED state and cant wait on delete: %v\n", err)
			return 4
		}
	}

	pp := &cloudformation.CreateStackInput{
		StackName:       aws.String(stack),
		Capabilities:    []types.Capability{types.CapabilityCapabilityNamedIam, types.CapabilityCapabilityAutoExpand}, // NOTE
		DisableRollback: aws.Bool(norb),
		Parameters:      cfpp,
		Tags:            tagpp,
		TemplateBody:    aws.String(string(b)),
	}
	if len(arns) > 0 {
		pp.NotificationARNs = arns
	}
	_, err = s.cli.CreateStack(context.TODO(), pp)

	if err != nil {
		if errors.Is(err, &types.NameAlreadyExistsException{}) {
			fmt.Println("YO") // TODO the bit that doesn't work :(
		}
		fmt.Fprintf(os.Stderr, "cant create stack: %v\ndescribestacks err: %v\n", err, dserr)
		return 3
	}

	if dots || events {
		err := s.block(stack, dots, events)
		fmt.Println() // HAHA YUCKY
		if err != nil {
			fmt.Fprintf(os.Stderr, "error on wait: %v\n", err)
			return 1
		}
	}

	if outPipe {
		fmt.Println(stack)
	}
	return 0
}

func (s stack) remv(args []string, force bool, wait string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "rm accepts one positional argument, the name of the stack")
		fmt.Print(usageRemv)
		return 64
	}
	if force {
		// TODO
		fmt.Fprintln(os.Stderr, "-force is not yet implemented - you're on your own for now!")
	}
	stack := args[0]
	dots := wait == "dots"
	events := wait == "events"

	h := sfm.Handle{CFNcli: s.cli}
	if _, err := h.Delete(stack); err != nil {
		fmt.Fprintf(os.Stderr, "cant delete stack: %v\n", err)
		return 1
	}

	if dots || events {
		err := s.block(stack, dots, events)
		fmt.Println() // OMG GROSS
		if err != nil {
			fmt.Fprintf(os.Stderr, "error on wait: %v\n", err)
			return 1
		}
	}

	if isPiped() {
		fmt.Println(stack)
	}

	return 0
}

func (s stack) wait(args []string, dots, events bool) int {
	if dots && events {
		fmt.Fprintln(os.Stderr, "-dots and -events are mutually exclusive flags; choose one")
		fmt.Print(usageWait)
		return 64
	}
	stack := ""
	if havePipe() {
		b, _ := io.ReadAll(os.Stdin)
		stack = strings.TrimSpace(string(b))
	}
	if stack == "" {
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "wait requires a stack name on stdin or as the only positional argument")
			fmt.Print(usageWait)
			return 64
		}
		stack = args[0]
	}

	err := s.block(stack, dots, events)
	if dots {
		fmt.Println()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error on wait: %v\n", err)
		return 1
	}
	return 0
}

// WARN this func prints to stdout and shit
func (s stack) block(name string, dots, events bool) error {
	t := time.Now().UTC()
	seen := []time.Time{} // used by -events
	pp := &cloudformation.DescribeStacksInput{StackName: aws.String(name)}
	ppev := &cloudformation.DescribeStackEventsInput{StackName: aws.String(name)}

	var in = func(tt []time.Time, ts time.Time) bool {
		for _, t := range tt {
			if ts == t {
				return true
			}
		}
		return false
	}

	i := 0
	for {
		if i > (30 * 60) { // 2 second sleep (see end of loop) * 30 loops = 1 minute * 60 loops = 1 hour (minimum due to possible backoffdelay/retry on api rate limit)
			return fmt.Errorf("timeout waiting on stack")
		}

		o, err := s.cli.DescribeStacks(context.TODO(), pp)
		if err != nil {
			return nil
		}
		if len(o.Stacks) < 1 {
			return nil
		}
		st := status(o.Stacks[0].StackStatus)
		if st != "prog" {
			if st != "ok" {
				return fmt.Errorf("stack status not 'ok': %s (%s)", o.Stacks[0].StackStatus, st)
			}
			return nil
		}

		if events {
			eo, err := s.cli.DescribeStackEvents(context.TODO(), ppev)
			if err != nil {
				time.Sleep(2 * time.Second)
				continue
			}
			for j := len(eo.StackEvents) - 1; j >= 0; j-- {
				e := eo.StackEvents[j]
				if e.Timestamp.Before(t) || in(seen, *e.Timestamp) {
					continue
				}

				lri := "-"
				if e.LogicalResourceId != nil {
					lri = *e.LogicalResourceId
				}
				if len(lri) > 30 {
					lri = lri[0:27] + "..."
				}

				rs := e.ResourceStatus
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
				if e.ResourceStatusReason != nil {
					rsr = *e.ResourceStatusReason
				}

				loc, _ := time.LoadLocation("Local") // WARN this might break on non-UNIX systems
				tsf := e.Timestamp.In(loc).Format("15:04:05 MST")

				// fmt.Printf("%s\t%s\t%s\t%s\n", e.ResourceStatus, rsr, lri, *e.ResourceType)
				fmt.Printf("%s %-30s %s%-20s%s %s\n", tsf, lri, rsColor, rs, cReset, rsr)
				seen = append(seen, *e.Timestamp)
			}
		}

		if dots {
			fmt.Print(".")
		}
		time.Sleep(2 * time.Second)
		i++
	}
}

func (s stack) stat(args []string, outputs, params, tags, res bool, encoding string) int {
	stack := ""
	if havePipe() {
		b, _ := io.ReadAll(os.Stdin)
		stack = strings.TrimSpace(string(b))
	}
	if stack == "" {
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "stat requires a stack name on stdin or as the only positional argument")
			fmt.Print(usageStat)
			return 64
		}
		stack = args[0]
	}

	h := sfm.Handle{CFNcli: s.cli}
	x, err := h.Get(stack)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cant stat stack: %v\n", err)
		return 1
	}

	oo := map[string]string{}
	switch {
	case outputs:
		oo = x.Outputs
	case params:
		oo = x.Params
	case tags:
		oo = x.Tags
	case res:
		mm, err := x.Resources()
		if err != nil {
			fmt.Fprintf(os.Stderr, "cant get resources: %v\n", err)
			return 1
		}
		for id, r := range mm {
			oo[id] = r["pid"]
		}
	}

	if outputs || params || tags || res {
		o, err := outputter(encoding, oo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		fmt.Print(o)
		return 0
	}

	switch encoding {
	case "yaml", "yml":
		b, err := yaml.Marshal(x)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cant marshal Stack struct to yaml: %v\n", err)
			return 1
		}
		fmt.Println(string(b))
		return 0
	case "json":
		b, err := json.Marshal(x)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cant marshal Stack struct to json: %v\n", err)
			return 1
		}
		fmt.Println(string(b))
		return 0
	case "text":
		caps := strings.Join(x.Caps, ", ")
		topics := strings.Join(x.Topics, ", ")
		updated := ""
		if !x.Updated.IsZero() {
			updated = x.Updated.String()
		}

		fmts := "Description\t%s\nCreationTime\t%s\nUpdateTime\t%s\nStackStatus\t%s\nStatusReason\t%s\nCapabilities\t%s\nDisableRollback\t%v\nTermProtection\t%v\nNotificationARNs\t%s\n"
		fmt.Printf(fmts, x.Desc, x.Created, updated, x.Status, x.Reason, caps,
			x.NoRollback, x.TermProc, topics)

		return 0
	}
	fmt.Fprintf(os.Stderr, "unknown encoding '%s'\n", encoding)
	return 1
}

func havePipe() bool {
	s, _ := os.Stdin.Stat()
	return (s.Mode() & os.ModeCharDevice) == 0
}

func isPiped() bool {
	s, _ := os.Stdout.Stat()
	return (s.Mode() & os.ModeCharDevice) == 0
}

func outputter(enc string, m map[string]string) (string, error) {
	o := ""
	switch enc {
	case "text":
		for k, v := range m {
			o += fmt.Sprintf("%s\t%s\n", k, v)
		}
		return o, nil
	case "yaml", "yml":
		b, err := yaml.Marshal(m)
		if err != nil {
			return "", fmt.Errorf("cant marshal to yaml: %w", err)
		}
		return "---\n" + string(b), nil
	case "json":
		b, err := json.Marshal(m)
		if err != nil {
			return "", fmt.Errorf("cant marshal to json: %w", err)
		}
		return string(b) + "\n", nil
	}

	return "", errors.New("unknown encoding: " + enc)
}

func status(s types.StackStatus) string {
	switch s {
	case types.StackStatusCreateComplete,
		types.StackStatusImportComplete,
		types.StackStatusDeleteComplete,
		types.StackStatusUpdateComplete:
		return "ok"
	case types.StackStatusCreateInProgress,
		types.StackStatusDeleteInProgress,
		types.StackStatusImportInProgress,
		types.StackStatusImportRollbackInProgress,
		types.StackStatusReviewInProgress,
		types.StackStatusRollbackInProgress,
		types.StackStatusUpdateCompleteCleanupInProgress,
		types.StackStatusUpdateInProgress,
		types.StackStatusUpdateRollbackCompleteCleanupInProgress,
		types.StackStatusUpdateRollbackInProgress:
		return "prog"
	}
	return "err"
}

func loadYamlFile(fn string) (map[string]string, error) {
	if fn == "" {
		// You didn't give me a file path so I won't do anything
		return map[string]string{}, nil
	}
	bb, err := os.ReadFile(filepath.Clean(fn))
	if err != nil {
		return nil, fmt.Errorf("can't read file %s: %w", fn, err)
	}

	var i interface{}
	err = yaml.Unmarshal(bb, &i)
	if err != nil {
		return nil, fmt.Errorf("can't unmarshal file %s: %w", fn, err)
	}

	res := map[string]string{}
	// TODO panics on bad input, add check
	for k, v := range i.(map[interface{}]interface{}) {
		key := k.(string)
		// handle normal 'key: value`
		if val, ok := v.(string); ok {
			res[key] = val
			continue
		}
		// handle yaml thinking this `key: value` was a bool (yaml.v2 treats `yes/no`, `true/false`, `on/off` as bool)
		if val, ok := v.(bool); ok {
			if val {
				res[key] = "True"
				continue
			}
			res[key] = "False"
			continue
		}
		// handle lists of values, 'key: \n  - value 1\n  - value 2'
		if valslice, ok := v.([]interface{}); ok {
			var vals []string
			for _, valinterface := range valslice {
				if val, ok := valinterface.(string); ok {
					vals = append(vals, val)
				}
				if val, ok := v.(bool); ok {
					if val {
						vals = append(vals, "True")
					}
					vals = append(vals, "False")
				}
			}
			csv := strings.Join(vals, ",")
			res[key] = csv
			continue
		}
		return nil, fmt.Errorf("can't load in file, something wrong with key %s", key)
	}
	return res, nil
}

func openS3(cfg aws.Config, path string) (*bytes.Buffer, error) {
	u, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("cant parse url '%s': %w", path, err)
	}
	bucket := u.Hostname()
	key := strings.TrimPrefix(u.Path, "/")

	buf := bytes.Buffer{}
	cli := s3.NewFromConfig(cfg)
	o, err := cli.GetObject(
		context.Background(),
		&s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("cant get object from 's3://%s/%s': %w", bucket, key, err)
	}

	_, err = buf.ReadFrom(o.Body)
	return &buf, err

}

// types.StackStatusCreateInProgress                        StackStatus = "CREATE_IN_PROGRESS"
// types.StackStatusCreateFailed                            StackStatus = "CREATE_FAILED"
// types.StackStatusCreateComplete                          StackStatus = "CREATE_COMPLETE"
// types.StackStatusRollbackInProgress                      StackStatus = "ROLLBACK_IN_PROGRESS"
// types.StackStatusRollbackFailed                          StackStatus = "ROLLBACK_FAILED"
// types.StackStatusRollbackComplete                        StackStatus = "ROLLBACK_COMPLETE"
// types.StackStatusDeleteInProgress                        StackStatus = "DELETE_IN_PROGRESS"
// types.StackStatusDeleteFailed                            StackStatus = "DELETE_FAILED"
// types.StackStatusDeleteComplete                          StackStatus = "DELETE_COMPLETE"
// types.StackStatusUpdateInProgress                        StackStatus = "UPDATE_IN_PROGRESS"
// types.StackStatusUpdateCompleteCleanupInProgress         StackStatus = "UPDATE_COMPLETE_CLEANUP_IN_PROGRESS"
// types.StackStatusUpdateComplete                          StackStatus = "UPDATE_COMPLETE"
// types.StackStatusUpdateRollbackInProgress                StackStatus = "UPDATE_ROLLBACK_IN_PROGRESS"
// types.StackStatusUpdateRollbackFailed                    StackStatus = "UPDATE_ROLLBACK_FAILED"
// types.StackStatusUpdateRollbackCompleteCleanupInProgress StackStatus = "UPDATE_ROLLBACK_COMPLETE_CLEANUP_IN_PROGRESS"
// types.StackStatusUpdateRollbackComplete                  StackStatus = "UPDATE_ROLLBACK_COMPLETE"
// types.StackStatusReviewInProgress                        StackStatus = "REVIEW_IN_PROGRESS"
// types.StackStatusImportInProgress                        StackStatus = "IMPORT_IN_PROGRESS"
// types.StackStatusImportComplete                          StackStatus = "IMPORT_COMPLETE"
// types.StackStatusImportRollbackInProgress                StackStatus = "IMPORT_ROLLBACK_IN_PROGRESS"
// types.StackStatusImportRollbackFailed                    StackStatus = "IMPORT_ROLLBACK_FAILED"
// types.StackStatusImportRollbackComplete                  StackStatus = "IMPORT_ROLLBACK_COMPLETE"

var cReset = "\033[0m"
var cRed = "\033[31m"
var cGreen = "\033[32m"
var cCyan = "\033[36m"

// var cYellow = "\033[33m"
// var cBlue = "\033[34m"
// var cPurple = "\033[35m"
// var cGray = "\033[37m"
// var cWhite = "\033[97m"

func init() {
	if runtime.GOOS == "windows" {
		cReset = ""
		cRed = ""
		cGreen = ""
		cCyan = ""
		// cYellow = ""
		// cBlue = ""
		// cPurple = ""
		// cGray = ""
		// cWhite = ""
	}
}

const usageTop = `┌─┐┌┬┐┌─┐┌─┐┬┌─┌─┐┌─┐┬─┐┌┬┐
└─┐ │ ├─┤│  ├┴┐├┤ │ │├┬┘│││
└─┘ ┴ ┴ ┴└─┘┴ ┴└  └─┘┴└─┴ ┴
----------------------------------------------------------------------


Usage
  sfm [-h|-v] [-r] <subcommand> [-flags/args...]

  -r  set the aws region manually
  -h  display this help and exit
  -v  display the program version and exit

Summary
  sfm is sugar for managing cloudformation stacks, improving the ux in scripts
  and in interactive sessions.
  sfm is pipe friendly, output is tab-separated key-value-pairs by default
  which integrates well with 'cut' and 'column'.
  coarse-grained, domain-specific subcommands reduce cognitive complexity.

Sub-Commands
  ls    list stacks
  mk    create or update a stack
  rm    delete a stack
  wait  block on a stack while it's "in progress"
  stat  print information about a stack

  use <subcommand> -h for subcommand-specific help

Using sfm in Pipes
  some sfm subcommands support pipes:

  mk    accepts template content on stdin
        prints the stack name to stdout on create or update
  rm    prints the stack name to stdout on delete
  wait  reads the stack name from stdin
  stat  reads the stack name from stdin

Examples
  aws s3 cp s3://bucket/tmpl.yml - | sfm mk foobar | sfm wait -dots
  # same as above
  sfm mk -t s3://bucket/tmpl.yml -wait dots foobar

  sfm rm foobar | sfm wait -events
  # same as above
  sfm rm -wait events foobar
`

const usageList = `usage: sfm ls [-h|-v] [<glob>]

Summary
  prints all of the stack names in the account.

Flags
  -h      display this help
  -v      print the create or update (latest) time, the name, and the status
  <glob>  filter results by glob (see Go filepath.Match for supported globs)
`

const usageMake = `usage: sfm mk [-h] [-t <file>] [-p k=v,k=v...] [-wait style] <name>
   or: sfm mk [-p k=v,k=v...] <name> <file (template on stdin)

Summary
  mk is the heavy-duty operator in sfm - it creates or updates cloudformation
  stacks. a stack is created if it does not already exist, updated otherwise.
  in conjunction with the -wait flag, sfm exits non-zero if the stack fails to
  create or update. without -wait, a non-zero exit code is only returned if the
  cloudformation createstack api responds with an error.

Parameters
  parameters can be specified in two ways:
    - the -p flag takes a string of parameters in the form
      'key=value,key=value..."
      note: don't use this if your keys or values contain '=' or ','
    - the -pf flag takes a path to a json or yaml document describing the
      parameters. the document should be of the form
      '{"key":"value","key":"value",...}'
      note: -pf can be supplied multiple times - in this case, the files
      are processed in-order and later keys overwrite earlier ones

Flags
  -h               display this help
  -t <file>        provide a path to the template file
                   the template can also be passed in via stdin
  -p <string>      a list of key/value pairs separated by commas and equals
                   e.g., -p k1=v1,k2=v2,k3=v3
  -pf <file>       a path to a yaml file containing parameters
                   parameters provided by '-p' override the parameter file
                   can be specified multiple times; processed in order, keys overwrite
  -tags <string>   a list of key/value pairs separated by command and equals
                   e.g., -tags tag1=val1,tag2=val2
  -tagsfile <file> a path to a yaml file containing tags
                   tags provided by '-tags' override the tagsfile
  -wait <style>    block on the operation with either 'dots' or 'events'
                   any other value will be quiet
  <name>           the name of the stack
`

const usageRemv = `usage: sfm rm [-h] [-force] [-wait style] <name>

Summary
  this subcommand removes (deletes) a stack.

Flags
  -h             display this help
  -force         NOT IMPLEMENTED
  -wait <style>  block on the operation with either 'dots' or 'events'
                 any other value will be quiet
  <name>         the name of the stack to delete
`

const usageWait = `usage: sfm wait [-h] [-dots|-events] <name>

Flags
  -h      display this help
  -dots   print dots periodically while waiting
  -events print stack events while waiting (BROKEN ON RM)
  <name>  the name of the stack to wait on
          this value can come from stdin:
          e.g., sfm mk ... | sfm wait -dots
`

const usageStat = `usage: sfm stat [-h] [-o|-p|-t|-r] [-e encoding] <name>

Flags
  -h             display this help
  -o             print stack outputs
  -p             print stack parameters
  -t             print stack tags
  -r             print stack resources (logical and physical ids)
  -e <encoding>  encode the output (default 'text')
                 supports 'yaml','json','text'; 'text' is tab-sep
  <name>         the name of the stack to wait on
                 this value can come from stdin:
                 e.g., sfm mk ... | sfm wait -dots | sfm stat
`
