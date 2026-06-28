package feishu

import (
	"bytes"
	"context"
	"fmt"
	"io"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type feishuSendAPI struct {
	p *Platform
}

type feishuMessageAPILabels struct {
	retry   string
	network string
	failed  string
}

func (p *Platform) sendAPI() feishuSendAPI {
	return feishuSendAPI{p: p}
}

func (api feishuSendAPI) withRetry(ctx context.Context, operation string, fn feishuRequestFunc) error {
	return api.p.withTransientRetry(ctx, operation, func() error {
		return api.p.withFreshTenantAccessTokenRetry(ctx, operation, fn)
	})
}

func (api feishuSendAPI) replyMessage(ctx context.Context, rc replyContext, msgType, content string, labels feishuMessageAPILabels) (string, error) {
	req := larkim.NewReplyMessageReqBuilder().
		MessageId(rc.messageID).
		Body(api.p.buildReplyMessageReqBody(rc, msgType, content)).
		Build()

	var resp *larkim.ReplyMessageResp
	if err := api.withRetry(ctx, labels.retry, func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
		var err error
		resp, err = client.Im.Message.Reply(ctx, req, options...)
		if err != nil {
			return fmt.Errorf("%s: %s: %w", api.p.tag(), labels.network, err)
		}
		if !resp.Success() {
			return fmt.Errorf("%s: %s code=%d msg=%s", api.p.tag(), labels.failed, resp.Code, resp.Msg)
		}
		return nil
	}); err != nil {
		return "", err
	}
	if resp.Data != nil && resp.Data.MessageId != nil {
		return *resp.Data.MessageId, nil
	}
	return "", nil
}

func (api feishuSendAPI) createMessage(ctx context.Context, chatID, msgType, content string, labels feishuMessageAPILabels) (string, error) {
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType(msgType).
			Content(content).
			Build()).
		Build()

	var resp *larkim.CreateMessageResp
	if err := api.withRetry(ctx, labels.retry, func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
		var err error
		resp, err = client.Im.Message.Create(ctx, req, options...)
		if err != nil {
			return fmt.Errorf("%s: %s: %w", api.p.tag(), labels.network, err)
		}
		if !resp.Success() {
			return fmt.Errorf("%s: %s code=%d msg=%s", api.p.tag(), labels.failed, resp.Code, resp.Msg)
		}
		return nil
	}); err != nil {
		return "", err
	}
	if resp.Data != nil && resp.Data.MessageId != nil {
		return *resp.Data.MessageId, nil
	}
	return "", nil
}

func (api feishuSendAPI) patchMessage(ctx context.Context, messageID, content string, labels feishuMessageAPILabels) error {
	req := larkim.NewPatchMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewPatchMessageReqBodyBuilder().
			Content(content).
			Build()).
		Build()

	return api.withRetry(ctx, labels.retry, func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
		resp, err := client.Im.Message.Patch(ctx, req, options...)
		if err != nil {
			return fmt.Errorf("%s: %s: %w", api.p.tag(), labels.network, err)
		}
		if !resp.Success() {
			return fmt.Errorf("%s: %s code=%d msg=%s", api.p.tag(), labels.failed, resp.Code, resp.Msg)
		}
		return nil
	})
}

func (api feishuSendAPI) patchMessageOnce(ctx context.Context, messageID, content string, labels feishuMessageAPILabels) error {
	req := larkim.NewPatchMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewPatchMessageReqBodyBuilder().
			Content(content).
			Build()).
		Build()

	resp, err := api.p.client.Im.Message.Patch(ctx, req)
	if err != nil {
		return fmt.Errorf("%s: %s: %w", api.p.tag(), labels.network, err)
	}
	if !resp.Success() {
		return fmt.Errorf("%s: %s code=%d msg=%s", api.p.tag(), labels.failed, resp.Code, resp.Msg)
	}
	return nil
}

func (api feishuSendAPI) uploadImageKey(ctx context.Context, data []byte) (string, error) {
	var resp *larkim.CreateImageResp
	if err := api.withRetry(ctx, "upload image", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
		req := larkim.NewCreateImageReqBuilder().
			Body(larkim.NewCreateImageReqBodyBuilder().
				ImageType("message").
				Image(bytes.NewReader(data)).
				Build()).
			Build()
		var err error
		resp, err = client.Im.Image.Create(ctx, req, options...)
		if err != nil {
			return fmt.Errorf("%s: upload image: %w", api.p.tag(), err)
		}
		if !resp.Success() {
			return fmt.Errorf("%s: upload image code=%d msg=%s", api.p.tag(), resp.Code, resp.Msg)
		}
		return nil
	}); err != nil {
		return "", err
	}
	if resp.Data == nil || resp.Data.ImageKey == nil {
		return "", fmt.Errorf("%s: upload image: no image_key returned", api.p.tag())
	}
	return *resp.Data.ImageKey, nil
}

func (api feishuSendAPI) uploadFileKey(ctx context.Context, operation, fileType, fileName string, data []byte) (string, error) {
	var resp *larkim.CreateFileResp
	if err := api.withRetry(ctx, operation, func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
		req := larkim.NewCreateFileReqBuilder().
			Body(larkim.NewCreateFileReqBodyBuilder().
				FileType(fileType).
				FileName(fileName).
				File(bytes.NewReader(data)).
				Build()).
			Build()
		var err error
		resp, err = client.Im.File.Create(ctx, req, options...)
		if err != nil {
			return fmt.Errorf("%s: %s: %w", api.p.tag(), operation, err)
		}
		if !resp.Success() {
			return fmt.Errorf("%s: %s code=%d msg=%s", api.p.tag(), operation, resp.Code, resp.Msg)
		}
		return nil
	}); err != nil {
		return "", err
	}
	if resp.Data == nil || resp.Data.FileKey == nil {
		return "", fmt.Errorf("%s: %s: no file_key returned", api.p.tag(), operation)
	}
	return *resp.Data.FileKey, nil
}

func (api feishuSendAPI) downloadMessageResource(ctx context.Context, messageID, fileKey, resType, operation string) ([]byte, error) {
	resp, err := api.p.client.Im.MessageResource.Get(ctx,
		larkim.NewGetMessageResourceReqBuilder().
			MessageId(messageID).
			FileKey(fileKey).
			Type(resType).
			Build())
	if err != nil {
		return nil, fmt.Errorf("%s: %s: %w", api.p.tag(), operation, err)
	}
	if !resp.Success() {
		return nil, fmt.Errorf("%s: %s code=%d msg=%s", api.p.tag(), operation, resp.Code, resp.Msg)
	}
	if resp.File == nil {
		return nil, fmt.Errorf("%s: %s returned nil file body", api.p.tag(), operation)
	}
	data, err := io.ReadAll(resp.File)
	if err != nil {
		return nil, fmt.Errorf("%s: read %s: %w", api.p.tag(), resourceReadName(operation), err)
	}
	return data, nil
}

func resourceReadName(operation string) string {
	switch operation {
	case "image API":
		return "image"
	case "resource API":
		return "resource"
	default:
		return operation
	}
}
