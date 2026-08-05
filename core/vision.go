package core

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// VisionSettings configures the "vision fallback" path: when the primary
// model cannot read images (e.g. deepseek-* text-only models), cc-connect
// sends the images to a dedicated vision model first, then feeds the
// resulting text description back to the primary model so the conversation
// keeps flowing instead of failing with "model does not support images".
//
// Configured via [projects.agent.options]:
//
//	vision_api_base_url = "https://opencode.ai/zen/v1"
//	vision_api_key      = "..."
//	vision_model        = "mimo-v2.5-free"
//	vision_fallback     = "auto" | "always" | "never"   (default "auto")
type VisionSettings struct {
	BaseURL string
	APIKey  string
	Model   string
	Mode    VisionFallbackMode
}

// VisionFallbackMode controls when images are routed through the vision model.
type VisionFallbackMode string

const (
	// VisionFallbackAuto falls back when the primary model is not known to
	// support images natively.
	VisionFallbackAuto VisionFallbackMode = "auto"
	// VisionFallbackAlways always routes images through the vision model,
	// even if the primary model could read them directly.
	VisionFallbackAlways VisionFallbackMode = "always"
	// VisionFallbackNever disables the vision fallback entirely.
	VisionFallbackNever VisionFallbackMode = "never"
)

// Enabled reports whether the vision fallback has a usable configuration.
func (v VisionSettings) Enabled() bool {
	return v.BaseURL != "" && v.APIKey != "" && v.Model != ""
}

// NeedsFallback reports whether images should be routed through the vision
// model for the given primary model name.
func (v VisionSettings) NeedsFallback(model string) bool {
	if !v.Enabled() {
		return false
	}
	switch v.Mode {
	case VisionFallbackAlways:
		return true
	case VisionFallbackNever:
		return false
	default:
		return !ModelSupportsVision(model)
	}
}

// ParseVisionFallback normalizes a raw config value into a fallback mode.
// Empty or unknown values default to auto.
func ParseVisionFallback(raw any) VisionFallbackMode {
	switch strings.ToLower(strings.TrimSpace(fmt.Sprint(raw))) {
	case "always", "true", "1", "on":
		return VisionFallbackAlways
	case "never", "false", "0", "off":
		return VisionFallbackNever
	default:
		return VisionFallbackAuto
	}
}

// ModelSupportsVision reports whether the given model name is known to accept
// image inputs natively. Unknown models conservatively report false so the
// configured vision fallback takes over instead of the primary model failing
// on an image message.
func ModelSupportsVision(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return false
	}
	markers := []string{
		"gpt-4o", "gpt-4.1", "gpt-4-vision", "gpt-4-turbo", "gpt-5", "o3",
		"gemini", "claude", "kimi", "glm-4v", "glm-5v",
		"qwen-vl", "qwen2-vl", "qwen2.5-vl", "qwen3-vl",
		"pixtral", "llava", "gemma-3", "moondream", "internvl", "cogvlm", "minicpm-v",
		"grok-2-vision", "grok-4", "mimo", "step-1v", "doubao-1.5-vision", "hunyuan-vision",
		"vision",
	}
	for _, marker := range markers {
		if strings.Contains(m, marker) {
			return true
		}
	}
	return false
}

const (
	visionMaxImageBytes  = 12 * 1024 * 1024 // 12 MiB raw image cap (base64 inflates to ~16 MiB)
	visionRequestTimeout = 120 * time.Second
)

type visionPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *visionImageURL `json:"image_url,omitempty"`
}

type visionImageURL struct {
	URL string `json:"url"`
}

// DescribeImages sends the given images to the configured vision model and
// returns the model's text description. The request uses OpenAI-compatible
// chat completions with data-URL image parts, so it works with any gateway
// exposing a /v1/chat/completions endpoint (e.g. the opencode zen gateway).
func DescribeImages(ctx context.Context, cfg VisionSettings, prompt string, images []ImageAttachment, maxTokens int) (string, error) {
	if !cfg.Enabled() {
		return "", fmt.Errorf("vision fallback is not configured (need vision_api_base_url, vision_api_key, vision_model)")
	}
	if len(images) == 0 {
		return "", fmt.Errorf("no images to describe")
	}
	if strings.TrimSpace(prompt) == "" {
		prompt = "请按顺序详细描述这张图片的内容，包括文字、数字、界面元素与布局。"
	}
	if maxTokens <= 0 {
		maxTokens = 2048
	}

	parts := make([]visionPart, 0, 1+len(images))
	parts = append(parts, visionPart{Type: "text", Text: prompt})
	for i, img := range images {
		if len(img.Data) > visionMaxImageBytes {
			return "", fmt.Errorf("图片 %d 大小 %.1f MiB 超过视觉模型上限 12 MiB", i+1, float64(len(img.Data))/(1024*1024))
		}
		mime := strings.TrimSpace(img.MimeType)
		if mime == "" {
			mime = "image/png"
		}
		dataURL := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
		parts = append(parts, visionPart{Type: "image_url", ImageURL: &visionImageURL{URL: dataURL}})
	}

	body, err := json.Marshal(map[string]any{
		"model":      cfg.Model,
		"messages":   []map[string]any{{"role": "user", "content": parts}},
		"max_tokens": maxTokens,
	})
	if err != nil {
		return "", fmt.Errorf("vision request marshal: %w", err)
	}

	url := strings.TrimRight(cfg.BaseURL, "/")
	if !strings.HasSuffix(url, "/chat/completions") {
		url += "/chat/completions"
	}

	client := &http.Client{Timeout: visionRequestTimeout}
	reqCtx := ctx
	if reqCtx == nil {
		reqCtx = context.Background()
	}

	// The gateway occasionally returns an empty completion or a transient 5xx
	// on the first try, so retry once before surfacing an error.
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(3 * time.Second):
			case <-reqCtx.Done():
				return "", reqCtx.Err()
			}
		}
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("vision request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("vision API 请求失败: %w", err)
			continue
		}
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("vision API 响应读取失败: %w", readErr)
			continue
		}
		if resp.StatusCode >= 500 {
			snippet := string(respBody)
			if len(snippet) > 500 {
				snippet = snippet[:500]
			}
			lastErr = fmt.Errorf("vision API 返回 %d: %s", resp.StatusCode, strings.TrimSpace(snippet))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			snippet := string(respBody)
			if len(snippet) > 500 {
				snippet = snippet[:500]
			}
			return "", fmt.Errorf("vision API 返回 %d: %s", resp.StatusCode, strings.TrimSpace(snippet))
		}

		var parsed struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			lastErr = fmt.Errorf("vision API 响应解析失败: %w", err)
			continue
		}
		if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
			lastErr = fmt.Errorf("vision API 返回内容为空")
			continue
		}
		return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
	}
	return "", lastErr
}
