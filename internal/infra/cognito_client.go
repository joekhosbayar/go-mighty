package infra

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	cip "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
)

// CognitoAttributesFetcher looks up user attributes via AdminGetUser using
// the ambient AWS credential chain (instance role in prod).
type CognitoAttributesFetcher struct {
	client *cip.Client
	poolID string
}

// NewCognitoAttributesFetcher creates a fetcher backed by the AWS Cognito
// Identity Provider API for the given region and user pool.
func NewCognitoAttributesFetcher(ctx context.Context, region, poolID string) (*CognitoAttributesFetcher, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, err
	}

	return &CognitoAttributesFetcher{client: cip.NewFromConfig(cfg), poolID: poolID}, nil
}

// PreferredUsername returns the user's preferred_username attribute, or ""
// if absent. For email-sign-in pools the Cognito username IS the sub.
func (f *CognitoAttributesFetcher) PreferredUsername(ctx context.Context, sub string) (string, error) {
	out, err := f.client.AdminGetUser(ctx, &cip.AdminGetUserInput{
		UserPoolId: aws.String(f.poolID),
		Username:   aws.String(sub),
	})
	if err != nil {
		return "", err
	}

	for _, attr := range out.UserAttributes {
		if aws.ToString(attr.Name) == "preferred_username" {
			return aws.ToString(attr.Value), nil
		}
	}

	return "", nil
}
