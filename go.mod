module github.com/toolsdotgo/sfm

go 1.20

replace github.com/toolsdotgo/sfm/pkg/sfm => ./pkg/sfm

require (
	github.com/aws/aws-sdk-go-v2 v1.29.0
	github.com/aws/aws-sdk-go-v2/config v1.27.20
	github.com/aws/aws-sdk-go-v2/service/cloudformation v1.52.0
	github.com/aws/aws-sdk-go-v2/service/s3 v1.55.2
	github.com/toolsdotgo/sfm/pkg/sfm v0.0.0-20220124042655-90327d37d619
	gopkg.in/yaml.v2 v2.4.0
)

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.6.2 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.17.20 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.16.7 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.3.11 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.6.11 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.0 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.3.10 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.11.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.3.12 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.11.13 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.17.10 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.21.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.25.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.29.0 // indirect
	github.com/aws/smithy-go v1.20.2 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
)
