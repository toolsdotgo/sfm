package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/toolsdotgo/sfm/pkg/sfm"
)

func main() {
	cfg, err := config.LoadDefaultConfig(
		context.Background(),
		config.WithRetryer(func() aws.Retryer {
			retryer := retry.AddWithMaxAttempts(retry.NewStandard(), 10)
			return retry.AddWithMaxBackoffDelay(retryer, 30*time.Second)
		}),
	)
	if err != nil {
		panic(err)
	}
	h, err := sfm.NewHandle(cfg)
	if err != nil {
		panic(err)
	}

	name := "sfm-pkg-test-cli-1"
	timeout := 60 * time.Minute
	var test = func(tmpl string) error {
		s := h.NewStack(name)
		if err := s.NewTemplate([]byte(tmpl)); err != nil {
			return err
		}

		token, err := h.Make(s)
		if err != nil {
			return err
		}

		id := ""
		for start := time.Now(); time.Since(start) < timeout; {
			s, err := h.Get(name)
			if err != nil {
				return err
			}
			ee, err := s.Events(id, token)
			if err != nil {
				return err
			}
			for _, e := range ee {
				fmt.Println(e.Pretty())
				id = e.ID
			}
			if s.Short == "ok" {
				return nil
			}
			if s.Short == "err" {
				return errors.New("stack in err state")
			}
			time.Sleep(2 * time.Second)
		}

		return errors.New("timeout")
	}

	err = test(`---
AWSTemplateFormatVersion: 2010-09-09
Description: sfm package testing ahoyhoy

Resources:
  bucket:
    Type: AWS::S3::Bucket
`)
	if err != nil {
		panic(err)
	}

	err = test(`---
AWSTemplateFormatVersion: 2010-09-09
Description: sfm package testing ahoyhoy

Resources:
  bucket:
    Type: AWS::S3::Bucket
    Properties:
      AccessControl: Private
`)
	if err != nil {
		panic(err)
	}

	_, err = h.Delete(name)
	if err != nil {
		panic(err)
	}
}
