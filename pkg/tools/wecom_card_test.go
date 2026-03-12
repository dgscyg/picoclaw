package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestBuildWecomCardPayload_TextNotice(t *testing.T) {
	payload, result := buildWecomCardPayload(map[string]any{
		"card_type": "text_notice",
		"main_title": map[string]any{
			"title": "测试卡片",
		},
		"sub_title_text": "二级说明",
		"card_action": map[string]any{
			"type": 1,
			"url":  "https://work.weixin.qq.com/",
		},
	})
	if result != nil {
		t.Fatalf("buildWecomCardPayload() error = %+v", result)
	}

	templateCard := payload["template_card"].(map[string]any)
	if got := templateCard["card_type"]; got != "text_notice" {
		t.Fatalf("card_type = %v", got)
	}
	cardAction := templateCard["card_action"].(map[string]any)
	if got := cardAction["type"]; got != 1 {
		t.Fatalf("card_action.type = %v", got)
	}
	if got := cardAction["url"]; got != "https://work.weixin.qq.com/" {
		t.Fatalf("card_action.url = %v", got)
	}
}

func TestBuildWecomCardPayload_NewsNotice(t *testing.T) {
	payload, result := buildWecomCardPayload(map[string]any{
		"card_type": "news_notice",
		"main_title": map[string]any{
			"title": "图文通知",
			"desc":  "摘要",
		},
		"card_image": map[string]any{
			"url":          "https://example.com/card.png",
			"aspect_ratio": 1.5,
		},
		"card_action": map[string]any{
			"type": 1,
			"url":  "https://example.com/detail",
		},
	})
	if result != nil {
		t.Fatalf("buildWecomCardPayload() error = %+v", result)
	}

	templateCard := payload["template_card"].(map[string]any)
	if _, ok := templateCard["card_image"].(map[string]any); !ok {
		t.Fatalf("card_image missing: %#v", templateCard)
	}
	cardAction := templateCard["card_action"].(map[string]any)
	if got := cardAction["type"]; got != 1 {
		t.Fatalf("card_action.type = %v", got)
	}
}

func TestBuildWecomCardPayload_ButtonInteractionAllowsCardActionTypeZero(t *testing.T) {
	payload, result := buildWecomCardPayload(map[string]any{
		"card_type": "button_interaction",
		"main_title": map[string]any{
			"title": "按钮卡片",
		},
		"task_id": "task_button_1",
		"button_list": []any{
			map[string]any{
				"text": "确认",
				"key":  "confirm",
			},
			map[string]any{
				"text":  "误报",
				"style": 2,
				"key":   "false_alarm",
			},
		},
		"card_action": map[string]any{
			"type": 0,
		},
	})
	if result != nil {
		t.Fatalf("buildWecomCardPayload() error = %+v", result)
	}

	templateCard := payload["template_card"].(map[string]any)
	cardAction := templateCard["card_action"].(map[string]any)
	if got := cardAction["type"]; got != 0 {
		t.Fatalf("card_action.type = %v", got)
	}
	if _, exists := cardAction["url"]; exists {
		t.Fatalf("card_action.url should be omitted when type=0: %#v", cardAction)
	}
}

func TestBuildWecomCardPayload_VoteInteraction(t *testing.T) {
	payload, result := buildWecomCardPayload(map[string]any{
		"card_type": "vote_interaction",
		"main_title": map[string]any{
			"title": "投票卡片",
		},
		"task_id": "task_vote_1",
		"checkbox": map[string]any{
			"question_key": "question-1",
			"mode":         1,
			"option_list": []any{
				map[string]any{"id": "a", "text": "A"},
				map[string]any{"id": "b", "text": "B", "is_checked": true},
			},
		},
		"submit_button": map[string]any{
			"text": "提交",
			"key":  "submit_vote",
		},
	})
	if result != nil {
		t.Fatalf("buildWecomCardPayload() error = %+v", result)
	}

	templateCard := payload["template_card"].(map[string]any)
	if _, ok := templateCard["checkbox"].(map[string]any); !ok {
		t.Fatalf("checkbox missing: %#v", templateCard)
	}
	if _, ok := templateCard["submit_button"].(map[string]any); !ok {
		t.Fatalf("submit_button missing: %#v", templateCard)
	}
}

func TestBuildWecomCardPayload_MultipleInteraction(t *testing.T) {
	payload, result := buildWecomCardPayload(map[string]any{
		"card_type": "multiple_interaction",
		"main_title": map[string]any{
			"title": "多项选择",
		},
		"select_list": []any{
			map[string]any{
				"question_key": "q1",
				"title":        "选择器1",
				"option_list": []any{
					map[string]any{"id": "a", "text": "选项A"},
					map[string]any{"id": "b", "text": "选项B"},
				},
			},
		},
		"submit_button": map[string]any{
			"text": "提交",
			"key":  "submit_multi",
		},
	})
	if result != nil {
		t.Fatalf("buildWecomCardPayload() error = %+v", result)
	}

	templateCard := payload["template_card"].(map[string]any)
	selectList := templateCard["select_list"].([]map[string]any)
	if len(selectList) != 1 {
		t.Fatalf("select_list len = %d", len(selectList))
	}
}

func TestBuildWecomCardPayload_RejectsTextNoticeCardActionTypeZero(t *testing.T) {
	_, result := buildWecomCardPayload(map[string]any{
		"card_type": "text_notice",
		"main_title": map[string]any{
			"title": "测试卡片",
		},
		"card_action": map[string]any{
			"type": 0,
		},
	})
	if result == nil || !result.IsError {
		t.Fatal("expected error result")
	}
	if result.ForLLM != "text_notice requires card_action.type to be 1 or 2" {
		t.Fatalf("ForLLM = %q", result.ForLLM)
	}
}

func TestBuildWecomCardPayload_IgnoresEmptyOptionalSections(t *testing.T) {
	payload, result := buildWecomCardPayload(map[string]any{
		"card_type": "text_notice",
		"main_title": map[string]any{
			"title": "测试卡片",
		},
		"sub_title_text": "这是一条企业微信卡片消息",
		"card_action": map[string]any{
			"type": 1,
			"url":  "https://example.com/card",
		},
		"action_menu": map[string]any{
			"desc":        "",
			"action_list": []any{},
		},
		"button_list": []any{},
		"button_selection": map[string]any{
			"question_key": "",
			"option_list":  []any{},
		},
		"checkbox": map[string]any{
			"question_key": "",
			"option_list":  []any{},
		},
		"submit_button": map[string]any{
			"text": "",
			"key":  "",
		},
		"select_list": []any{},
		"image_text_area": map[string]any{
			"image_url": "",
		},
		"card_image": map[string]any{
			"url": "",
		},
	})
	if result != nil {
		t.Fatalf("buildWecomCardPayload() error = %+v", result)
	}

	templateCard := payload["template_card"].(map[string]any)
	if _, exists := templateCard["action_menu"]; exists {
		t.Fatalf("action_menu should be omitted when empty: %#v", templateCard)
	}
	if _, exists := templateCard["button_list"]; exists {
		t.Fatalf("button_list should be omitted when empty: %#v", templateCard)
	}
}

func TestWecomCardTool_Execute_SendsPayloadAndMarksRound(t *testing.T) {
	tool := NewWecomCardTool()
	var sentChannel, sentChatID, sentContent string
	tool.SetSendCallback(func(ctx context.Context, channel, chatID, content string) error {
		sentChannel = channel
		sentChatID = chatID
		sentContent = content
		return nil
	})

	ctx := WithToolRoutingContext(context.Background(), "wecom_official", "YangXu", "reply-1")
	result := tool.Execute(ctx, map[string]any{
		"card_type": "text_notice",
		"main_title": map[string]any{
			"title": "测试卡片",
		},
		"sub_title_text": "二级说明",
		"card_action": map[string]any{
			"type": 1,
			"url":  "https://work.weixin.qq.com/",
		},
	})
	if !result.Silent || result.IsError {
		t.Fatalf("unexpected result: %+v", result)
	}
	if sentChannel != "wecom_official" || sentChatID != "YangXu" {
		t.Fatalf("sent target = %s:%s", sentChannel, sentChatID)
	}
	if !tool.HasSentInRound() {
		t.Fatal("expected wecom_card to mark the round as sent")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(sentContent), &payload); err != nil {
		t.Fatalf("unmarshal sent payload: %v", err)
	}
	if got := payload["msgtype"]; got != "template_card" {
		t.Fatalf("msgtype = %v", got)
	}
}
