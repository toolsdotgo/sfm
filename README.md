# sfm

`sfm` (stackform) is a modest `go` program that helps manage cloudformation stacks both interactively and in scripts.

# use

## make a stack

```
# basic usage
sfm mk -t cf/bucket.yml my-bucket
# creates a stack called 'my-bucket' using cf/bucket.yml as the template

# block until complete
sfm mk -t cf/stack.yml -wait events my-stack
# -wait accepts 'dots', 'events', or any other value for 'quiet'

# template on stdin
aws s3 cp s3://mybucket/edge.yml - | sfm mk -wait x my-stack
# pull a template from s3 and build it, blocking quietly

# other things
sfm ls
sfm rm -wait dots your-stack
sfm stat -o my-stack
```

## other operations

`sfm` can perform other tasks - refer to the in-built help for more information.


# raw help

```
┌─┐┌┬┐┌─┐┌─┐┬┌─┌─┐┌─┐┬─┐┌┬┐
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
```
