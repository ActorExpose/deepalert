package inspector_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/google/uuid"
	"github.com/m-mizutani/deepalert"
	"github.com/m-mizutani/deepalert/inspector"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dummyInspector(ctx context.Context, attr deepalert.Attribute) (*deepalert.TaskResult, error) {
	// tableName := os.Getenv("RESULT_TABLE")
	// reportID, _ := deepalert.ReportIDFromCtx(ctx)

	hostReport := deepalert.ReportHost{
		IPAddr: []string{"10.1.2.3"},
		Owner:  []string{"superman"},
	}

	newAttr := deepalert.Attribute{
		Key:   "username",
		Value: "mizutani",
		Type:  deepalert.TypeUserName,
	}

	return &deepalert.TaskResult{
		Contents:      []deepalert.ReportContent{&hostReport},
		NewAttributes: []deepalert.Attribute{newAttr},
	}, nil
}

func TestInspectorHandler(t *testing.T) {
	result, err := inspector.StartTest(inspector.Arguments{
		Handler: dummyInspector,
		Author:  "dummyInspector",
	}, deepalert.Attribute{
		Type:  deepalert.TypeIPAddr,
		Key:   "SrcIP",
		Value: "10.0.0.1",
	})

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 1, len(result.Contents))
	assert.Equal(t, 1, len(result.NewAttributes))
}

type dummySQSClient struct {
	Requests []*sqs.SendMessageInput
}

func (x *dummySQSClient) SendMessage(req *sqs.SendMessageInput) (*sqs.SendMessageOutput, error) {
	x.Requests = append(x.Requests, req)
	return &sqs.SendMessageOutput{}, nil
}

func convert(src interface{}, dst interface{}) error {
	raw, err := json.Marshal(src)
	if err != nil {
		return err
	}

	return json.Unmarshal(raw, dst)
}

func TestSQS(t *testing.T) {
	dummy := dummySQSClient{}
	inspector.InjectNewSQSClient(&dummy)
	defer inspector.FixNewSQSClient()

	attrURL := "https://sqs.ap-northeast-1.amazonaws.com/123456789xxx/attribute-queue"
	contentURL := "https://sqs.ap-northeast-1.amazonaws.com/123456789xxx/content-queue"
	args := inspector.Arguments{
		Handler:         dummyInspector,
		Author:          "blue",
		AttrQueueURL:    attrURL,
		ContentQueueURL: contentURL,
	}

	task := deepalert.Task{
		ReportID: deepalert.ReportID(uuid.New().String()),
		Attribute: deepalert.Attribute{
			Type:  deepalert.TypeIPAddr,
			Key:   "dst",
			Value: "192.10.0.1",
		},
	}

	err := inspector.HandleTask(context.Background(), args, task)
	require.NoError(t, err)
	assert.Equal(t, 2, len(dummy.Requests))

	var req1 deepalert.ReportSection
	err = json.Unmarshal([]byte(*dummy.Requests[0].MessageBody), &req1)
	require.NoError(t, err)
	assert.Equal(t, contentURL, aws.StringValue(dummy.Requests[0].QueueUrl))
	assert.Equal(t, attrURL, aws.StringValue(dummy.Requests[1].QueueUrl))

	var host deepalert.ReportHost
	require.NoError(t, convert(req1.Content, &host))
	assert.Equal(t, "10.1.2.3", host.IPAddr[0])
	assert.Equal(t, "superman", host.Owner[0])
}
