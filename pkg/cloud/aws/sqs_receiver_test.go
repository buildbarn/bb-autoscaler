package aws_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/smithy-go"
	"github.com/buildbarn/bb-autoscaler/internal/mock"
	as_aws "github.com/buildbarn/bb-autoscaler/pkg/cloud/aws"
	"github.com/buildbarn/bb-storage/pkg/program"
	"github.com/buildbarn/bb-storage/pkg/testutil"
	"github.com/stretchr/testify/require"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.uber.org/mock/gomock"
)

func TestSQSReceiver(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)

	sqsClient := mock.NewMockSQSClient(ctrl)
	messageHandler := mock.NewMockSQSMessageHandler(ctrl)
	errorLogger := mock.NewMockErrorLogger(ctrl)
	sr := as_aws.NewSQSReceiver(
		sqsClient,
		"https://sqs.eu-west-1.amazonaws.com/249843598229/MySQSQueue",
		10*time.Minute,
		messageHandler,
		errorLogger)

	t.Run("ReceiveMessageFailure", func(t *testing.T) {
		// Failures to read from SQS can be returned
		// immediately, as this happens in the foreground.
		sqsClient.EXPECT().ReceiveMessage(gomock.Any(), &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String("https://sqs.eu-west-1.amazonaws.com/249843598229/MySQSQueue"),
			MaxNumberOfMessages: 10,
			VisibilityTimeout:   600,
			WaitTimeSeconds:     20,
		}).Return(nil, &smithy.OperationError{
			ServiceID:     "sqs",
			OperationName: "ReceiveMessage",
			Err:           errors.New("received a HTTP 503"),
		})

		require.Equal(
			t,
			&smithy.OperationError{
				ServiceID:     "sqs",
				OperationName: "ReceiveMessage",
				Err:           errors.New("received a HTTP 503"),
			},
			program.RunLocal(ctx, func(ctx context.Context, siblingsGroup, dependenciesGroup program.Group) error {
				return sr.PerformSingleRequest(ctx, siblingsGroup)
			}))
	})

	t.Run("HandlerFailure", func(t *testing.T) {
		// Because handlers are executed asynchronously, any
		// errors should be passed to the provided ErrorLogger.
		sqsClient.EXPECT().ReceiveMessage(gomock.Any(), &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String("https://sqs.eu-west-1.amazonaws.com/249843598229/MySQSQueue"),
			MaxNumberOfMessages: 10,
			VisibilityTimeout:   600,
			WaitTimeSeconds:     20,
		}).Return(&sqs.ReceiveMessageOutput{
			Messages: []types.Message{
				{
					Body:          aws.String("This is a message body"),
					MessageId:     aws.String("8dcc80c7-83ed-4d3c-aa38-59342fd192f8"),
					ReceiptHandle: aws.String("15066bf8-c753-4788-813a-18e607d209f9"),
				},
			},
		}, nil)
		messageHandler.EXPECT().HandleMessage("This is a message body").
			Return(status.Error(codes.Internal, "Cannot contact backend service"))
		errorLogger.EXPECT().Log(testutil.EqStatus(t, status.Error(codes.Internal, "Failed to process message \"8dcc80c7-83ed-4d3c-aa38-59342fd192f8\": Cannot contact backend service")))

		require.NoError(t, program.RunLocal(ctx, func(ctx context.Context, siblingsGroup, dependenciesGroup program.Group) error {
			return sr.PerformSingleRequest(ctx, siblingsGroup)
		}))
	})

	t.Run("DeleteMessageFailure", func(t *testing.T) {
		// After processing the message, it should be deleted
		// from the queue. Deletion errors should also be passed
		// to the ErrorLogger.
		sqsClient.EXPECT().ReceiveMessage(gomock.Any(), &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String("https://sqs.eu-west-1.amazonaws.com/249843598229/MySQSQueue"),
			MaxNumberOfMessages: 10,
			VisibilityTimeout:   600,
			WaitTimeSeconds:     20,
		}).Return(&sqs.ReceiveMessageOutput{
			Messages: []types.Message{
				{
					Body:          aws.String("This is a message body"),
					MessageId:     aws.String("8dcc80c7-83ed-4d3c-aa38-59342fd192f8"),
					ReceiptHandle: aws.String("15066bf8-c753-4788-813a-18e607d209f9"),
				},
			},
		}, nil)
		messageHandler.EXPECT().HandleMessage("This is a message body")
		sqsClient.EXPECT().DeleteMessage(gomock.Any(), &sqs.DeleteMessageInput{
			QueueUrl:      aws.String("https://sqs.eu-west-1.amazonaws.com/249843598229/MySQSQueue"),
			ReceiptHandle: aws.String("15066bf8-c753-4788-813a-18e607d209f9"),
		}).Return(nil, &smithy.OperationError{
			ServiceID:     "sqs",
			OperationName: "DeleteMessage",
			Err:           errors.New("received a HTTP 503"),
		})
		errorLogger.EXPECT().Log(testutil.EqStatus(t, status.Error(codes.Internal, "Failed to delete message \"8dcc80c7-83ed-4d3c-aa38-59342fd192f8\": operation error sqs: DeleteMessage, received a HTTP 503")))

		require.NoError(t, program.RunLocal(ctx, func(ctx context.Context, siblingsGroup, dependenciesGroup program.Group) error {
			return sr.PerformSingleRequest(ctx, siblingsGroup)
		}))
	})

	t.Run("Success", func(t *testing.T) {
		// The full workflow, where two messages are received,
		// handled and deleted from the queue.
		sqsClient.EXPECT().ReceiveMessage(gomock.Any(), &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String("https://sqs.eu-west-1.amazonaws.com/249843598229/MySQSQueue"),
			MaxNumberOfMessages: 10,
			VisibilityTimeout:   600,
			WaitTimeSeconds:     20,
		}).Return(&sqs.ReceiveMessageOutput{
			Messages: []types.Message{
				{
					Body:          aws.String("This is a message body"),
					MessageId:     aws.String("8dcc80c7-83ed-4d3c-aa38-59342fd192f8"),
					ReceiptHandle: aws.String("15066bf8-c753-4788-813a-18e607d209f9"),
				},
				{
					Body:          aws.String("This is another message body"),
					MessageId:     aws.String("aacc1ff6-b2aa-4c9a-8fc9-f2a01354f7df"),
					ReceiptHandle: aws.String("6eef738e-ca40-449b-8771-f3604ebba993"),
				},
			},
		}, nil)

		messageHandler.EXPECT().HandleMessage("This is a message body")
		sqsClient.EXPECT().DeleteMessage(gomock.Any(), &sqs.DeleteMessageInput{
			QueueUrl:      aws.String("https://sqs.eu-west-1.amazonaws.com/249843598229/MySQSQueue"),
			ReceiptHandle: aws.String("15066bf8-c753-4788-813a-18e607d209f9"),
		}).Return(&sqs.DeleteMessageOutput{}, nil)

		messageHandler.EXPECT().HandleMessage("This is another message body")
		sqsClient.EXPECT().DeleteMessage(gomock.Any(), &sqs.DeleteMessageInput{
			QueueUrl:      aws.String("https://sqs.eu-west-1.amazonaws.com/249843598229/MySQSQueue"),
			ReceiptHandle: aws.String("6eef738e-ca40-449b-8771-f3604ebba993"),
		}).Return(&sqs.DeleteMessageOutput{}, nil)

		require.NoError(t, program.RunLocal(ctx, func(ctx context.Context, siblingsGroup, dependenciesGroup program.Group) error {
			return sr.PerformSingleRequest(ctx, siblingsGroup)
		}))
	})
}
