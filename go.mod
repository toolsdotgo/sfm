module github.com/toolsdotgo/sfm

go 1.20

replace github.com/toolsdotgo/sfm/pkg/sfm => ./pkg/sfm

require (
	github.com/aws/aws-sdk-go-v2 v1.20.3
	github.com/aws/aws-sdk-go-v2/config v1.18.35
	github.com/aws/aws-sdk-go-v2/service/cloudformation v1.34.3
	github.com/aws/aws-sdk-go-v2/service/s3 v1.38.4
	github.com/toolsdotgo/sfm/pkg/sfm v0.0.0-20220124042655-90327d37d619
	gopkg.in/yaml.v2 v2.4.0
)

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.4.13 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.13.34 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.13.10 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.1.40 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.4.34 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.3.41 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.1.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.9.14 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.1.35 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.9.34 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.15.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.13.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.15.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.21.4 // indirect
	github.com/aws/smithy-go v1.14.2 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
)
