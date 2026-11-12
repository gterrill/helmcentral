package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
)

// One image plus a prompt, sent to a vision model and read back: the call the
// document indexer (documents_enrich.go) and the Inbox (inbox_mate.go) both
// make. Each keeps its own prompt, timeout and wording of a failure; this file
// holds the part that must not drift between them.

// openRouterImageBlocks is the prompt as text, then the image as a data URL.
func openRouterImageBlocks(prompt, mime string, image []byte) []openRouterContentBlock {
	dataURL := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(image)
	return []openRouterContentBlock{
		{Type: "text", Text: prompt},
		{Type: "image_url", ImageURL: &openRouterImageURLBlock{URL: dataURL}},
	}
}

// openRouterFirstChoice makes one completion call and returns the response with
// its first choice's message. A reply with no choices is an error.
func openRouterFirstChoice(ctx context.Context, doer openRouterDoer, apiKey string, req openRouterChatRequest) (openRouterChatResponse, openRouterMessage, error) {
	resp, err := openRouterChatCompletionOnce(ctx, doer, apiKey, req)
	if err != nil {
		return resp, openRouterMessage{}, err
	}
	if len(resp.Choices) == 0 {
		return resp, openRouterMessage{}, errors.New("openrouter returned no choices")
	}
	return resp, resp.Choices[0].Message, nil
}

// decodeModelJSON decodes a model's reply as JSON, tolerating the one markdown
// fence some models add despite being asked not to.
func decodeModelJSON(raw string, into any) error {
	return json.Unmarshal([]byte(stripDocumentEnrichJSONFence(raw)), into)
}

// openRouterImageText sends one image with a prompt and returns the reply text.
func openRouterImageText(ctx context.Context, doer openRouterDoer, apiKey, model, prompt, mime string, image []byte) (string, error) {
	req := openRouterChatRequest{
		Model:    model,
		Messages: []openRouterMessage{openRouterUserMessage(openRouterImageBlocks(prompt, mime, image)...)},
	}
	_, msg, err := openRouterFirstChoice(ctx, doer, apiKey, req)
	if err != nil {
		return "", err
	}
	return string(msg.Content), nil
}
