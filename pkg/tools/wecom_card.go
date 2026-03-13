package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

type WecomCardSendCallback func(ctx context.Context, channel, chatID, content string) error

type WecomCardTool struct {
	sendCallback WecomCardSendCallback
	sentInRound  atomic.Bool
	defaultTitle string
}

var wecomCardTaskIDPattern = regexp.MustCompile(`^[A-Za-z0-9_@-]+$`)

func NewWecomCardTool() *WecomCardTool {
	return &WecomCardTool{}
}

func (t *WecomCardTool) SetSendCallback(callback WecomCardSendCallback) {
	t.sendCallback = callback
}

func (t *WecomCardTool) SetDefaultTitle(title string) {
	t.defaultTitle = strings.TrimSpace(title)
}

func (t *WecomCardTool) ResetSentInRound() {
	t.sentInRound.Store(false)
}

func (t *WecomCardTool) HasSentInRound() bool {
	return t.sentInRound.Load()
}

func (t *WecomCardTool) Name() string { return "wecom_card" }

func (t *WecomCardTool) Description() string {
	return "Generate and optionally send a native enterprise WeCom template card on `wecom_official`. Always use this instead of hand-writing raw `template_card` JSON. Supports the official `text_notice`, `news_notice`, `button_interaction`, `vote_interaction`, and `multiple_interaction` card types, validates card-type-specific rules such as `card_action`, `task_id`, list sizes, and required sections, and silently ignores empty optional objects or arrays. If the current callback is a `template_card_event`, use this tool only inside the official 5-second card update window; after that window expires, do not try to repair the old card and switch to a plain `message` follow-up in the same callback context instead. If the original `wecom_official` thinking placeholder or reply stream is already older than the official 6-minute stream edit window, do not use this tool to modify that old placeholder; send a normal `message` in the current reply context so the channel can deliver a fresh follow-up message instead. Decision policy: first infer user intent, then choose the card type. If the user explicitly wants an image card, picture card, image presentation card, company introduction card with image, or provides an image URL to be shown in the card, prefer `news_notice`; do not downgrade that request to `button_interaction` just to avoid jump requirements. If the user did not explicitly request a URL jump, website link, mini-program jump, navigation target, or click-to-open behavior, do not use `card_action.type=1` or `card_action.type=2`. In that no-jump case, do not choose `text_notice` or `news_notice`; prefer a non-jump interaction card, usually `button_interaction` with `card_action.type=0`. Exception: if the user explicitly provides a URL and also requests an image or presentation card, you may reuse that same user-provided URL as `card_action.url` for `news_notice`; do not invent a different URL. Never invent a URL, appid, or pagepath just to satisfy a required field. For vague requests like 'send a card' or 'test a card', default to the safest non-jump interaction card. Omit optional fields that are not actually needed, and do not send an extra plain message after a successful card unless the user explicitly asked for it. Delivery is still subject to official per-chat message limits."
}

func (t *WecomCardTool) Parameters() map[string]any {
	return objectSchema(
		map[string]any{
			"card_type": enumStringSchema(
				"Official template card type to generate. If the current trigger is a `template_card_event`, prefer updating the existing interactive card instead of sending a separate plain message. If the user explicitly asks for an image or picture card, prefer `news_notice`. If the user did not explicitly request a link or mini-program jump, do not choose `text_notice` or `news_notice`; prefer `button_interaction`, `vote_interaction`, or `multiple_interaction` instead. For vague requests like 'send a card', default to the safest non-jump interaction card.",
				"text_notice",
				"news_notice",
				"button_interaction",
				"vote_interaction",
				"multiple_interaction",
			),
			"send": boolSchema("Optional. Default true. When false, only generate the JSON payload and do not send it. When true in a current wecom_official reply context, the tool will use the official callback reply path when possible. If that card-update path has already expired, use the normal `message` tool for a follow-up instead of trying to force another card update."),
			"source": objectSchema(map[string]any{
				"icon_url": stringSchema("Optional source icon URL."),
				"desc":     stringSchema("Optional source description."),
				"desc_color": intEnumSchema(
					"Optional source description color: 0 default gray, 1 black, 2 red, 3 green.",
					0,
					1,
					2,
					3,
				),
			}),
			"action_menu": objectSchema(
				map[string]any{
					"desc": stringSchema("Description shown in the card action menu."),
					"action_list": arraySchema(
						"Optional list of 1-3 menu actions. When present, `task_id` is required.",
						objectSchema(
							map[string]any{
								"text": stringSchema("Menu action text."),
								"key":  stringSchema("Menu action callback key."),
							},
							"text",
							"key",
						),
					),
				},
				"desc",
				"action_list",
			),
			"main_title": objectSchema(map[string]any{
				"title": stringSchema("Main title text."),
				"desc":  stringSchema("Optional main title description."),
			}),
			"emphasis_content": objectSchema(map[string]any{
				"title": stringSchema("Optional emphasis title."),
				"desc":  stringSchema("Optional emphasis description."),
			}),
			"quote_area": objectSchema(map[string]any{
				"type": intEnumSchema(
					"Optional quote-area action type: 0 or omitted = none, 1 = URL, 2 = mini-program.",
					0,
					1,
					2,
				),
				"url":        stringSchema("Required when quote_area.type=1."),
				"appid":      stringSchema("Required when quote_area.type=2."),
				"pagepath":   stringSchema("Optional when quote_area.type=2."),
				"title":      stringSchema("Optional quote title."),
				"quote_text": stringSchema("Optional quote text."),
			}),
			"sub_title_text": stringSchema("Optional secondary text. For `text_notice`, either `main_title.title` or this field must be provided."),
			"horizontal_content_list": arraySchema(
				"Optional horizontal content list, max 6 items.",
				objectSchema(
					map[string]any{
						"type": intEnumSchema(
							"Optional item action type: 0 or omitted = plain text, 1 = URL, 3 = member profile.",
							0,
							1,
							3,
						),
						"keyname": stringSchema("Required key label."),
						"value":   stringSchema("Optional value text."),
						"url":     stringSchema("Required when type=1."),
						"userid":  stringSchema("Required when type=3."),
					},
					"keyname",
				),
			),
			"jump_list": arraySchema(
				"Optional jump list, max 3 items.",
				objectSchema(
					map[string]any{
						"type": intEnumSchema(
							"Optional jump type: 0 or omitted = no jump, 1 = URL, 2 = mini-program, 3 = smart reply question.",
							0,
							1,
							2,
							3,
						),
						"title":    stringSchema("Required jump title."),
						"question": stringSchema("Required when type=3."),
						"url":      stringSchema("Required when type=1."),
						"appid":    stringSchema("Required when type=2."),
						"pagepath": stringSchema("Optional when type=2."),
					},
					"title",
				),
			),
			"card_action": objectSchema(map[string]any{
				"type": intEnumSchema(
					"Overall card click action. 0 = no jump, 1 = URL, 2 = mini-program. For `text_notice` and `news_notice`, only 1 or 2 are valid. If the user did not explicitly ask for a jump, do not set this to 1 or 2. Never invent a URL or mini-program target just to satisfy this field.",
					0,
					1,
					2,
				),
				"url":      stringSchema("Required when card_action.type=1. Use only when the user explicitly requested a URL jump."),
				"appid":    stringSchema("Required when card_action.type=2. Use only when the user explicitly requested a mini-program jump."),
				"pagepath": stringSchema("Optional when card_action.type=2."),
			}),
			"task_id": stringSchema("Optional card task ID. Required for some card types and when `action_menu` is present."),
			"card_image": objectSchema(
				map[string]any{
					"url":          stringSchema("Required card image URL."),
					"aspect_ratio": numberSchema("Optional aspect ratio, should be >1.3 and <2.25."),
				},
				"url",
			),
			"image_text_area": objectSchema(
				map[string]any{
					"type": intEnumSchema(
						"Optional image-text action type: 0 or omitted = none, 1 = URL, 2 = mini-program.",
						0,
						1,
						2,
					),
					"url":       stringSchema("Required when image_text_area.type=1."),
					"appid":     stringSchema("Required when image_text_area.type=2."),
					"pagepath":  stringSchema("Optional when image_text_area.type=2."),
					"title":     stringSchema("Optional image-text title."),
					"desc":      stringSchema("Optional image-text description."),
					"image_url": stringSchema("Required image URL."),
				},
				"image_url",
			),
			"vertical_content_list": arraySchema(
				"Optional vertical content list, max 4 items.",
				objectSchema(
					map[string]any{
						"title": stringSchema("Required vertical content title."),
						"desc":  stringSchema("Optional vertical content description."),
					},
					"title",
				),
			),
			"button_selection": selectionItemSchema("Optional dropdown selector used by `button_interaction`."),
			"button_list": arraySchema(
				"Button list for `button_interaction`, 1-6 items.",
				objectSchema(
					map[string]any{
						"text":  stringSchema("Required button text."),
						"style": intEnumSchema("Optional button style, 1-4.", 1, 2, 3, 4),
						"key":   stringSchema("Required button callback key."),
					},
					"text",
					"key",
				),
			),
			"checkbox": objectSchema(
				map[string]any{
					"question_key": stringSchema("Required question key."),
					"disable":      boolSchema("Optional disable flag."),
					"mode":         intEnumSchema("Optional checkbox mode: 0 single choice, 1 multi choice.", 0, 1),
					"option_list": arraySchema(
						"Checkbox options, 1-20 items.",
						objectSchema(
							map[string]any{
								"id":         stringSchema("Required option ID."),
								"text":       stringSchema("Required option text."),
								"is_checked": boolSchema("Optional default checked state."),
							},
							"id",
							"text",
						),
					),
				},
				"question_key",
				"option_list",
			),
			"submit_button": objectSchema(
				map[string]any{
					"text": stringSchema("Required submit button text."),
					"key":  stringSchema("Required submit button key."),
				},
				"text",
				"key",
			),
			"select_list": arraySchema(
				"Selection list for `multiple_interaction`, 1-3 items.",
				selectionItemSchema("Selection item."),
			),
		},
		"card_type",
	)
}

func (t *WecomCardTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	payload, result := buildWecomCardPayloadWithDefaults(args, t.defaultTitle)
	if result != nil {
		return result
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to encode wecom card payload: %v", err))
	}

	send := true
	if rawSend, exists := args["send"]; exists && rawSend != nil {
		value, ok := rawSend.(bool)
		if !ok {
			return ErrorResult("send must be a boolean")
		}
		send = value
	}

	if !send {
		return SilentResult(fmt.Sprintf("Generated WeCom template_card JSON: %s", string(raw)))
	}

	channel := ToolChannel(ctx)
	chatID := ToolChatID(ctx)
	if channel != "wecom_official" {
		return ErrorResult("wecom_card can only send on the wecom_official channel")
	}
	if chatID == "" {
		return ErrorResult("no target chat available")
	}
	if t.sendCallback == nil {
		return ErrorResult("wecom_card sending not configured")
	}

	sendCtx := WithToolRoutingContext(ctx, channel, chatID, ToolReplyTo(ctx))
	if err := t.sendCallback(sendCtx, channel, chatID, string(raw)); err != nil {
		return ErrorResult(fmt.Sprintf("sending wecom card: %v", err)).WithError(err)
	}

	t.sentInRound.Store(true)
	return SilentResult(fmt.Sprintf("Generated and sent WeCom template_card JSON: %s", string(raw)))
}

func buildWecomCardPayload(args map[string]any) (map[string]any, *ToolResult) {
	return buildWecomCardPayloadWithDefaults(args, "")
}

func buildWecomCardPayloadWithDefaults(args map[string]any, defaultTitle string) (map[string]any, *ToolResult) {
	card, result := buildTemplateCard(args, strings.TrimSpace(defaultTitle))
	if result != nil {
		return nil, result
	}

	return map[string]any{
		"msgtype":       "template_card",
		"template_card": card,
	}, nil
}

func buildTemplateCard(args map[string]any, defaultTitle string) (map[string]any, *ToolResult) {
	cardType := strings.TrimSpace(strArg(args, "card_type"))
	if cardType == "" {
		return nil, ErrorResult("card_type is required")
	}

	switch cardType {
	case "text_notice", "news_notice", "button_interaction", "vote_interaction", "multiple_interaction":
	default:
		return nil, ErrorResult("unsupported card_type")
	}

	card := map[string]any{"card_type": cardType}

	source, result := buildSource(args, "source")
	if result != nil {
		return nil, result
	}
	if source != nil {
		card["source"] = source
	}

	actionMenu, result := buildActionMenu(args, "action_menu")
	if result != nil {
		return nil, result
	}
	if actionMenu != nil {
		card["action_menu"] = actionMenu
	}

	mainTitle, mainTitleTitle, result := buildMainTitle(args, "main_title")
	if result != nil {
		return nil, result
	}
	if mainTitle != nil {
		card["main_title"] = mainTitle
	}

	emphasisContent, result := buildSimpleTextPair(args, "emphasis_content", "emphasis_content")
	if result != nil {
		return nil, result
	}
	if emphasisContent != nil {
		card["emphasis_content"] = emphasisContent
	}

	quoteArea, result := buildQuoteArea(args, "quote_area")
	if result != nil {
		return nil, result
	}
	if quoteArea != nil {
		card["quote_area"] = quoteArea
	}

	subTitleText := strings.TrimSpace(strArg(args, "sub_title_text"))
	if subTitleText != "" {
		card["sub_title_text"] = subTitleText
	}

	horizontalList, result := buildHorizontalContentList(args, "horizontal_content_list")
	if result != nil {
		return nil, result
	}
	if len(horizontalList) > 0 {
		card["horizontal_content_list"] = horizontalList
	}

	jumpList, result := buildJumpList(args, "jump_list")
	if result != nil {
		return nil, result
	}
	if len(jumpList) > 0 {
		card["jump_list"] = jumpList
	}

	taskID := strings.TrimSpace(strArg(args, "task_id"))
	if taskID == "" && (actionMenu != nil || cardType == "button_interaction" || cardType == "vote_interaction") {
		taskID = generateWecomCardTaskID(cardType)
	}
	if taskID != "" {
		if result := validateTaskID(taskID); result != nil {
			return nil, result
		}
		card["task_id"] = taskID
	}
	if actionMenu != nil && taskID == "" {
		return nil, ErrorResult("task_id is required when action_menu is present")
	}

	switch cardType {
	case "text_notice":
		if strings.TrimSpace(mainTitleTitle) == "" && subTitleText == "" {
			return nil, ErrorResult("text_notice requires either main_title.title or sub_title_text")
		}
		cardAction, result := buildCardAction(args, "card_action", cardType, true)
		if result != nil {
			return nil, result
		}
		card["card_action"] = cardAction

	case "news_notice":
		if mainTitle == nil {
			return nil, ErrorResult("news_notice requires main_title")
		}

		cardImage, result := buildCardImage(args, "card_image")
		if result != nil {
			return nil, result
		}
		imageTextArea, result := buildImageTextArea(args, "image_text_area")
		if result != nil {
			return nil, result
		}
		if cardImage == nil && imageTextArea == nil {
			return nil, ErrorResult("news_notice requires either card_image or image_text_area")
		}
		if cardImage != nil {
			card["card_image"] = cardImage
		}
		if imageTextArea != nil {
			card["image_text_area"] = imageTextArea
		}

		verticalContentList, result := buildVerticalContentList(args, "vertical_content_list")
		if result != nil {
			return nil, result
		}
		if len(verticalContentList) > 0 {
			card["vertical_content_list"] = verticalContentList
		}

		cardAction, result := buildCardAction(args, "card_action", cardType, true)
		if result != nil {
			return nil, result
		}
		card["card_action"] = cardAction

	case "button_interaction":
		if mainTitle == nil {
			return nil, ErrorResult("button_interaction requires main_title")
		}
		if taskID == "" {
			return nil, ErrorResult("button_interaction requires task_id")
		}

		buttonSelection, result := buildSelectionItem(args, "button_selection", "button_selection")
		if result != nil {
			return nil, result
		}
		if buttonSelection != nil {
			card["button_selection"] = buttonSelection
		}

		buttonList, result := buildButtonList(args, "button_list")
		if result != nil {
			return nil, result
		}
		if len(buttonList) == 0 {
			return nil, ErrorResult("button_interaction requires button_list")
		}
		card["button_list"] = buttonList

		cardAction, result := buildCardAction(args, "card_action", cardType, false)
		if result != nil {
			return nil, result
		}
		if cardAction != nil {
			card["card_action"] = cardAction
		}

	case "vote_interaction":
		if mainTitle == nil {
			return nil, ErrorResult("vote_interaction requires main_title")
		}
		if taskID == "" {
			return nil, ErrorResult("vote_interaction requires task_id")
		}

		checkbox, result := buildCheckbox(args, "checkbox")
		if result != nil {
			return nil, result
		}
		if checkbox == nil {
			return nil, ErrorResult("vote_interaction requires checkbox")
		}
		card["checkbox"] = checkbox

		submitButton, result := buildSubmitButton(args, "submit_button")
		if result != nil {
			return nil, result
		}
		if submitButton == nil {
			return nil, ErrorResult("vote_interaction requires submit_button")
		}
		card["submit_button"] = submitButton

	case "multiple_interaction":
		if mainTitle == nil {
			return nil, ErrorResult("multiple_interaction requires main_title")
		}

		selectList, result := buildSelectList(args, "select_list")
		if result != nil {
			return nil, result
		}
		if len(selectList) == 0 {
			return nil, ErrorResult("multiple_interaction requires select_list")
		}
		card["select_list"] = selectList

		submitButton, result := buildSubmitButton(args, "submit_button")
		if result != nil {
			return nil, result
		}
		if submitButton == nil {
			return nil, ErrorResult("multiple_interaction requires submit_button")
		}
		card["submit_button"] = submitButton
	}

	applyWecomCardDefaultTitle(card, defaultTitle)

	return card, nil
}

func applyWecomCardDefaultTitle(card map[string]any, defaultTitle string) {
	defaultTitle = strings.TrimSpace(defaultTitle)
	if defaultTitle == "" {
		return
	}

	if source, ok := card["source"].(map[string]any); ok {
		desc, _ := source["desc"].(string)
		desc = strings.TrimSpace(desc)
		switch {
		case desc == "":
			source["desc"] = defaultTitle
		case strings.Contains(desc, "PicoClaw"):
			source["desc"] = strings.ReplaceAll(desc, "PicoClaw", defaultTitle)
		}
	} else {
		card["source"] = map[string]any{"desc": defaultTitle}
	}

	if mainTitle, ok := card["main_title"].(map[string]any); ok {
		title, _ := mainTitle["title"].(string)
		title = strings.TrimSpace(title)
		if strings.Contains(title, "PicoClaw") {
			mainTitle["title"] = strings.ReplaceAll(title, "PicoClaw", defaultTitle)
		}
		desc, _ := mainTitle["desc"].(string)
		desc = strings.TrimSpace(desc)
		if strings.Contains(desc, "PicoClaw") {
			mainTitle["desc"] = strings.ReplaceAll(desc, "PicoClaw", defaultTitle)
		}
	}

	if subTitle, _ := card["sub_title_text"].(string); strings.Contains(subTitle, "PicoClaw") {
		card["sub_title_text"] = strings.ReplaceAll(subTitle, "PicoClaw", defaultTitle)
	}
}

func generateWecomCardTaskID(cardType string) string {
	cardType = strings.TrimSpace(cardType)
	if cardType == "" {
		cardType = "card"
	}
	cardType = strings.ReplaceAll(cardType, "_interaction", "")
	cardType = strings.ReplaceAll(cardType, "_notice", "")
	cardType = strings.ReplaceAll(cardType, "_", "-")
	return fmt.Sprintf("%s-%s", cardType, time.Now().UTC().Format("20060102-150405"))
}

func buildSource(args map[string]any, key string) (map[string]any, *ToolResult) {
	sourceArg, exists, result := objectArg(args, key)
	if result != nil || !exists {
		return nil, result
	}
	if isEmptyCardValue(sourceArg) {
		return nil, nil
	}

	source := map[string]any{}
	if iconURL := strings.TrimSpace(strFromMap(sourceArg, "icon_url")); iconURL != "" {
		source["icon_url"] = iconURL
	}
	if desc := strings.TrimSpace(strFromMap(sourceArg, "desc")); desc != "" {
		source["desc"] = desc
	}
	if descColor, ok := coerceInt(sourceArg["desc_color"]); ok {
		if descColor < 0 || descColor > 3 {
			return nil, ErrorResult("source.desc_color must be 0, 1, 2, or 3")
		}
		source["desc_color"] = descColor
	}
	if len(source) == 0 {
		return nil, nil
	}
	return source, nil
}

func buildActionMenu(args map[string]any, key string) (map[string]any, *ToolResult) {
	menuArg, exists, result := objectArg(args, key)
	if result != nil || !exists {
		return nil, result
	}
	if isEmptyCardValue(menuArg) {
		return nil, nil
	}

	desc := strings.TrimSpace(strFromMap(menuArg, "desc"))
	listArg, listExists, result := arrayArg(menuArg, "action_list")
	if result != nil {
		return nil, result
	}
	if desc == "" && (!listExists || len(listArg) == 0) {
		return nil, nil
	}
	if desc == "" {
		return nil, ErrorResult("action_menu.desc is required")
	}

	if !listExists || len(listArg) == 0 || len(listArg) > 3 {
		return nil, ErrorResult("action_menu.action_list must contain 1 to 3 items")
	}

	actionList := make([]map[string]any, 0, len(listArg))
	for i, item := range listArg {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, ErrorResult(fmt.Sprintf("action_menu.action_list[%d] must be an object", i))
		}
		text := strings.TrimSpace(strFromMap(entry, "text"))
		actionKey := strings.TrimSpace(strFromMap(entry, "key"))
		if text == "" || actionKey == "" {
			return nil, ErrorResult(fmt.Sprintf("action_menu.action_list[%d] requires text and key", i))
		}
		actionList = append(actionList, map[string]any{
			"text": text,
			"key":  actionKey,
		})
	}

	return map[string]any{
		"desc":        desc,
		"action_list": actionList,
	}, nil
}

func buildMainTitle(args map[string]any, key string) (map[string]any, string, *ToolResult) {
	titleArg, exists, result := objectArg(args, key)
	if result != nil || !exists {
		return nil, "", result
	}
	if isEmptyCardValue(titleArg) {
		return nil, "", nil
	}

	title := strings.TrimSpace(strFromMap(titleArg, "title"))
	desc := strings.TrimSpace(strFromMap(titleArg, "desc"))
	if title == "" && desc == "" {
		return nil, "", ErrorResult("main_title must include title or desc")
	}

	mainTitle := map[string]any{}
	if title != "" {
		mainTitle["title"] = title
	}
	if desc != "" {
		mainTitle["desc"] = desc
	}

	return mainTitle, title, nil
}

func buildSimpleTextPair(args map[string]any, key string, label string) (map[string]any, *ToolResult) {
	valueArg, exists, result := objectArg(args, key)
	if result != nil || !exists {
		return nil, result
	}
	if isEmptyCardValue(valueArg) {
		return nil, nil
	}

	title := strings.TrimSpace(strFromMap(valueArg, "title"))
	desc := strings.TrimSpace(strFromMap(valueArg, "desc"))
	if title == "" && desc == "" {
		return nil, ErrorResult(fmt.Sprintf("%s must include title or desc", label))
	}

	value := map[string]any{}
	if title != "" {
		value["title"] = title
	}
	if desc != "" {
		value["desc"] = desc
	}

	return value, nil
}

func buildQuoteArea(args map[string]any, key string) (map[string]any, *ToolResult) {
	quoteArg, exists, result := objectArg(args, key)
	if result != nil || !exists {
		return nil, result
	}

	return buildLinkableArea(quoteArg, "quote_area", true)
}

func buildCardImage(args map[string]any, key string) (map[string]any, *ToolResult) {
	imageArg, exists, result := objectArg(args, key)
	if result != nil || !exists {
		return nil, result
	}
	if isEmptyCardValue(imageArg) {
		return nil, nil
	}

	url := strings.TrimSpace(strFromMap(imageArg, "url"))
	if url == "" {
		return nil, ErrorResult("card_image.url is required")
	}

	image := map[string]any{"url": url}
	if ratio, ok := coerceNumber(imageArg["aspect_ratio"]); ok {
		image["aspect_ratio"] = ratio
	}
	return image, nil
}

func buildImageTextArea(args map[string]any, key string) (map[string]any, *ToolResult) {
	areaArg, exists, result := objectArg(args, key)
	if result != nil || !exists {
		return nil, result
	}
	if isEmptyCardValue(areaArg) {
		return nil, nil
	}

	area, result := buildLinkableArea(areaArg, "image_text_area", false)
	if result != nil {
		return nil, result
	}
	if area == nil {
		area = map[string]any{}
	}

	imageURL := strings.TrimSpace(strFromMap(areaArg, "image_url"))
	if imageURL == "" {
		return nil, ErrorResult("image_text_area.image_url is required")
	}
	area["image_url"] = imageURL
	if title := strings.TrimSpace(strFromMap(areaArg, "title")); title != "" {
		area["title"] = title
	}
	if desc := strings.TrimSpace(strFromMap(areaArg, "desc")); desc != "" {
		area["desc"] = desc
	}
	return area, nil
}

func buildLinkableArea(areaArg map[string]any, label string, allowQuoteText bool) (map[string]any, *ToolResult) {
	area := map[string]any{}
	actionType, ok := coerceInt(areaArg["type"])
	if !ok {
		actionType = 0
	}
	if actionType < 0 || actionType > 2 {
		return nil, ErrorResult(fmt.Sprintf("%s.type must be 0, 1, or 2", label))
	}

	if actionType == 1 {
		url := strings.TrimSpace(strFromMap(areaArg, "url"))
		if url == "" {
			return nil, ErrorResult(fmt.Sprintf("%s.url is required when type=1", label))
		}
		area["type"] = actionType
		area["url"] = url
	} else if actionType == 2 {
		appID := strings.TrimSpace(strFromMap(areaArg, "appid"))
		if appID == "" {
			return nil, ErrorResult(fmt.Sprintf("%s.appid is required when type=2", label))
		}
		area["type"] = actionType
		area["appid"] = appID
		if pagepath := strings.TrimSpace(strFromMap(areaArg, "pagepath")); pagepath != "" {
			area["pagepath"] = pagepath
		}
	} else if hasNonEmptyString(areaArg, "url", "appid", "pagepath") {
		area["type"] = 0
	}

	if title := strings.TrimSpace(strFromMap(areaArg, "title")); title != "" {
		area["title"] = title
	}
	if allowQuoteText {
		if quoteText := strings.TrimSpace(strFromMap(areaArg, "quote_text")); quoteText != "" {
			area["quote_text"] = quoteText
		}
	}

	if len(area) == 0 {
		return nil, nil
	}
	return area, nil
}

func buildHorizontalContentList(args map[string]any, key string) ([]map[string]any, *ToolResult) {
	listArg, exists, result := arrayArg(args, key)
	if result != nil || !exists {
		return nil, result
	}
	if len(listArg) == 0 {
		return nil, nil
	}
	if len(listArg) > 6 {
		return nil, ErrorResult("horizontal_content_list cannot exceed 6 items")
	}

	entries := make([]map[string]any, 0, len(listArg))
	for i, item := range listArg {
		entryArg, ok := item.(map[string]any)
		if !ok {
			return nil, ErrorResult(fmt.Sprintf("horizontal_content_list[%d] must be an object", i))
		}

		keyname := strings.TrimSpace(strFromMap(entryArg, "keyname"))
		if keyname == "" {
			return nil, ErrorResult(fmt.Sprintf("horizontal_content_list[%d].keyname is required", i))
		}

		entry := map[string]any{"keyname": keyname}
		if value := strings.TrimSpace(strFromMap(entryArg, "value")); value != "" {
			entry["value"] = value
		}

		entryType, ok := coerceInt(entryArg["type"])
		if !ok {
			entryType = 0
		}
		switch entryType {
		case 0:
		case 1:
			url := strings.TrimSpace(strFromMap(entryArg, "url"))
			if url == "" {
				return nil, ErrorResult(fmt.Sprintf("horizontal_content_list[%d].url is required when type=1", i))
			}
			entry["type"] = entryType
			entry["url"] = url
		case 3:
			userID := strings.TrimSpace(strFromMap(entryArg, "userid"))
			if userID == "" {
				return nil, ErrorResult(fmt.Sprintf("horizontal_content_list[%d].userid is required when type=3", i))
			}
			entry["type"] = entryType
			entry["userid"] = userID
		default:
			return nil, ErrorResult(fmt.Sprintf("horizontal_content_list[%d].type must be 0, 1, or 3", i))
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

func buildJumpList(args map[string]any, key string) ([]map[string]any, *ToolResult) {
	listArg, exists, result := arrayArg(args, key)
	if result != nil || !exists {
		return nil, result
	}
	if len(listArg) == 0 {
		return nil, nil
	}
	if len(listArg) > 3 {
		return nil, ErrorResult("jump_list cannot exceed 3 items")
	}

	entries := make([]map[string]any, 0, len(listArg))
	for i, item := range listArg {
		entryArg, ok := item.(map[string]any)
		if !ok {
			return nil, ErrorResult(fmt.Sprintf("jump_list[%d] must be an object", i))
		}

		title := strings.TrimSpace(strFromMap(entryArg, "title"))
		if title == "" {
			return nil, ErrorResult(fmt.Sprintf("jump_list[%d].title is required", i))
		}

		entry := map[string]any{"title": title}
		entryType, ok := coerceInt(entryArg["type"])
		if !ok {
			entryType = 0
		}
		switch entryType {
		case 0:
		case 1:
			url := strings.TrimSpace(strFromMap(entryArg, "url"))
			if url == "" {
				return nil, ErrorResult(fmt.Sprintf("jump_list[%d].url is required when type=1", i))
			}
			entry["type"] = entryType
			entry["url"] = url
		case 2:
			appID := strings.TrimSpace(strFromMap(entryArg, "appid"))
			if appID == "" {
				return nil, ErrorResult(fmt.Sprintf("jump_list[%d].appid is required when type=2", i))
			}
			entry["type"] = entryType
			entry["appid"] = appID
			if pagepath := strings.TrimSpace(strFromMap(entryArg, "pagepath")); pagepath != "" {
				entry["pagepath"] = pagepath
			}
		case 3:
			question := strings.TrimSpace(strFromMap(entryArg, "question"))
			if question == "" {
				return nil, ErrorResult(fmt.Sprintf("jump_list[%d].question is required when type=3", i))
			}
			entry["type"] = entryType
			entry["question"] = question
		default:
			return nil, ErrorResult(fmt.Sprintf("jump_list[%d].type must be 0, 1, 2, or 3", i))
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

func buildCardAction(args map[string]any, key string, cardType string, required bool) (map[string]any, *ToolResult) {
	actionArg, exists, result := objectArg(args, key)
	if result != nil {
		return nil, result
	}
	if !exists {
		if required {
			return nil, ErrorResult(fmt.Sprintf("%s requires card_action", cardType))
		}
		return nil, nil
	}
	if _, hasType := actionArg["type"]; !hasType && !hasNonEmptyString(actionArg, "url", "appid", "pagepath") {
		if required {
			return nil, ErrorResult(fmt.Sprintf("%s requires card_action", cardType))
		}
		return nil, nil
	}

	actionType, ok := coerceInt(actionArg["type"])
	if !ok {
		actionType = 0
	}

	action := map[string]any{"type": actionType}
	switch cardType {
	case "text_notice", "news_notice":
		if actionType != 1 && actionType != 2 {
			return nil, ErrorResult(fmt.Sprintf("%s requires card_action.type to be 1 or 2", cardType))
		}
	default:
		if actionType < 0 || actionType > 2 {
			return nil, ErrorResult("card_action.type must be 0, 1, or 2")
		}
	}

	switch actionType {
	case 0:
	case 1:
		url := strings.TrimSpace(strFromMap(actionArg, "url"))
		if url == "" {
			return nil, ErrorResult("card_action.url is required when type=1")
		}
		action["url"] = url
	case 2:
		appID := strings.TrimSpace(strFromMap(actionArg, "appid"))
		if appID == "" {
			return nil, ErrorResult("card_action.appid is required when type=2")
		}
		action["appid"] = appID
		if pagepath := strings.TrimSpace(strFromMap(actionArg, "pagepath")); pagepath != "" {
			action["pagepath"] = pagepath
		}
	}

	return action, nil
}

func buildVerticalContentList(args map[string]any, key string) ([]map[string]any, *ToolResult) {
	listArg, exists, result := arrayArg(args, key)
	if result != nil || !exists {
		return nil, result
	}
	if len(listArg) == 0 {
		return nil, nil
	}
	if len(listArg) > 4 {
		return nil, ErrorResult("vertical_content_list cannot exceed 4 items")
	}

	entries := make([]map[string]any, 0, len(listArg))
	for i, item := range listArg {
		entryArg, ok := item.(map[string]any)
		if !ok {
			return nil, ErrorResult(fmt.Sprintf("vertical_content_list[%d] must be an object", i))
		}
		title := strings.TrimSpace(strFromMap(entryArg, "title"))
		if title == "" {
			return nil, ErrorResult(fmt.Sprintf("vertical_content_list[%d].title is required", i))
		}
		entry := map[string]any{"title": title}
		if desc := strings.TrimSpace(strFromMap(entryArg, "desc")); desc != "" {
			entry["desc"] = desc
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

func buildSelectionItem(args map[string]any, key string, label string) (map[string]any, *ToolResult) {
	itemArg, exists, result := objectArg(args, key)
	if result != nil || !exists {
		return nil, result
	}
	if isEmptyCardValue(itemArg) {
		return nil, nil
	}

	questionKey := strings.TrimSpace(strFromMap(itemArg, "question_key"))
	if questionKey == "" {
		return nil, ErrorResult(fmt.Sprintf("%s.question_key is required", label))
	}

	optionListArg, exists, result := arrayArg(itemArg, "option_list")
	if result != nil {
		return nil, result
	}
	if !exists || len(optionListArg) == 0 || len(optionListArg) > 10 {
		return nil, ErrorResult(fmt.Sprintf("%s.option_list must contain 1 to 10 items", label))
	}

	optionList := make([]map[string]any, 0, len(optionListArg))
	for i, item := range optionListArg {
		entryArg, ok := item.(map[string]any)
		if !ok {
			return nil, ErrorResult(fmt.Sprintf("%s.option_list[%d] must be an object", label, i))
		}
		id := strings.TrimSpace(strFromMap(entryArg, "id"))
		text := strings.TrimSpace(strFromMap(entryArg, "text"))
		if id == "" || text == "" {
			return nil, ErrorResult(fmt.Sprintf("%s.option_list[%d] requires id and text", label, i))
		}
		optionList = append(optionList, map[string]any{
			"id":   id,
			"text": text,
		})
	}

	selection := map[string]any{
		"question_key": questionKey,
		"option_list":  optionList,
	}
	if title := strings.TrimSpace(strFromMap(itemArg, "title")); title != "" {
		selection["title"] = title
	}
	if disable, ok := itemArg["disable"].(bool); ok {
		selection["disable"] = disable
	}
	if selectedID := strings.TrimSpace(strFromMap(itemArg, "selected_id")); selectedID != "" {
		selection["selected_id"] = selectedID
	}

	return selection, nil
}

func buildButtonList(args map[string]any, key string) ([]map[string]any, *ToolResult) {
	listArg, exists, result := arrayArg(args, key)
	if result != nil || !exists {
		return nil, result
	}
	if len(listArg) == 0 {
		return nil, nil
	}
	if len(listArg) > 6 {
		return nil, ErrorResult("button_list must contain 1 to 6 items")
	}

	buttons := make([]map[string]any, 0, len(listArg))
	for i, item := range listArg {
		entryArg, ok := item.(map[string]any)
		if !ok {
			return nil, ErrorResult(fmt.Sprintf("button_list[%d] must be an object", i))
		}
		text := strings.TrimSpace(strFromMap(entryArg, "text"))
		buttonKey := strings.TrimSpace(strFromMap(entryArg, "key"))
		if text == "" || buttonKey == "" {
			return nil, ErrorResult(fmt.Sprintf("button_list[%d] requires text and key", i))
		}
		button := map[string]any{
			"text": text,
			"key":  buttonKey,
		}
		if style, ok := coerceInt(entryArg["style"]); ok {
			if style < 1 || style > 4 {
				return nil, ErrorResult(fmt.Sprintf("button_list[%d].style must be between 1 and 4", i))
			}
			button["style"] = style
		}
		buttons = append(buttons, button)
	}

	return buttons, nil
}

func buildCheckbox(args map[string]any, key string) (map[string]any, *ToolResult) {
	checkboxArg, exists, result := objectArg(args, key)
	if result != nil || !exists {
		return nil, result
	}
	if isEmptyCardValue(checkboxArg) {
		return nil, nil
	}

	questionKey := strings.TrimSpace(strFromMap(checkboxArg, "question_key"))
	if questionKey == "" {
		return nil, ErrorResult("checkbox.question_key is required")
	}

	optionListArg, exists, result := arrayArg(checkboxArg, "option_list")
	if result != nil {
		return nil, result
	}
	if !exists || len(optionListArg) == 0 || len(optionListArg) > 20 {
		return nil, ErrorResult("checkbox.option_list must contain 1 to 20 items")
	}

	optionList := make([]map[string]any, 0, len(optionListArg))
	for i, item := range optionListArg {
		entryArg, ok := item.(map[string]any)
		if !ok {
			return nil, ErrorResult(fmt.Sprintf("checkbox.option_list[%d] must be an object", i))
		}
		id := strings.TrimSpace(strFromMap(entryArg, "id"))
		text := strings.TrimSpace(strFromMap(entryArg, "text"))
		if id == "" || text == "" {
			return nil, ErrorResult(fmt.Sprintf("checkbox.option_list[%d] requires id and text", i))
		}

		entry := map[string]any{
			"id":   id,
			"text": text,
		}
		if isChecked, ok := entryArg["is_checked"].(bool); ok {
			entry["is_checked"] = isChecked
		}
		optionList = append(optionList, entry)
	}

	checkbox := map[string]any{
		"question_key": questionKey,
		"option_list":  optionList,
	}
	if disable, ok := checkboxArg["disable"].(bool); ok {
		checkbox["disable"] = disable
	}
	if mode, ok := coerceInt(checkboxArg["mode"]); ok {
		if mode != 0 && mode != 1 {
			return nil, ErrorResult("checkbox.mode must be 0 or 1")
		}
		checkbox["mode"] = mode
	}

	return checkbox, nil
}

func buildSubmitButton(args map[string]any, key string) (map[string]any, *ToolResult) {
	buttonArg, exists, result := objectArg(args, key)
	if result != nil || !exists {
		return nil, result
	}
	if isEmptyCardValue(buttonArg) {
		return nil, nil
	}

	text := strings.TrimSpace(strFromMap(buttonArg, "text"))
	buttonKey := strings.TrimSpace(strFromMap(buttonArg, "key"))
	if text == "" || buttonKey == "" {
		return nil, ErrorResult("submit_button requires text and key")
	}
	return map[string]any{
		"text": text,
		"key":  buttonKey,
	}, nil
}

func buildSelectList(args map[string]any, key string) ([]map[string]any, *ToolResult) {
	listArg, exists, result := arrayArg(args, key)
	if result != nil || !exists {
		return nil, result
	}
	if len(listArg) == 0 {
		return nil, nil
	}
	if len(listArg) > 3 {
		return nil, ErrorResult("select_list must contain 1 to 3 items")
	}

	items := make([]map[string]any, 0, len(listArg))
	for i, item := range listArg {
		entryArg, ok := item.(map[string]any)
		if !ok {
			return nil, ErrorResult(fmt.Sprintf("select_list[%d] must be an object", i))
		}
		entry, result := buildSelectionItem(map[string]any{"tmp": entryArg}, "tmp", fmt.Sprintf("select_list[%d]", i))
		if result != nil {
			return nil, result
		}
		items = append(items, entry)
	}

	return items, nil
}

func validateTaskID(taskID string) *ToolResult {
	if len(taskID) > 128 {
		return ErrorResult("task_id must be 128 bytes or fewer")
	}
	if !wecomCardTaskIDPattern.MatchString(taskID) {
		return ErrorResult("task_id may contain only letters, numbers, '_', '-', and '@'")
	}
	return nil
}

func objectArg(args map[string]any, key string) (map[string]any, bool, *ToolResult) {
	value, exists := args[key]
	if !exists || value == nil {
		return nil, false, nil
	}
	objectValue, ok := value.(map[string]any)
	if !ok {
		return nil, true, ErrorResult(fmt.Sprintf("%s must be an object", key))
	}
	return objectValue, true, nil
}

func arrayArg(args map[string]any, key string) ([]any, bool, *ToolResult) {
	value, exists := args[key]
	if !exists || value == nil {
		return nil, false, nil
	}
	arrayValue, ok := value.([]any)
	if !ok {
		return nil, true, ErrorResult(fmt.Sprintf("%s must be an array", key))
	}
	return arrayValue, true, nil
}

func strArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

func strFromMap(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

func hasNonEmptyString(args map[string]any, keys ...string) bool {
	for _, key := range keys {
		if strings.TrimSpace(strFromMap(args, key)) != "" {
			return true
		}
	}
	return false
}

func isEmptyCardValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case bool:
		return !typed
	case int:
		return typed == 0
	case int32:
		return typed == 0
	case int64:
		return typed == 0
	case float32:
		return typed == 0
	case float64:
		return typed == 0
	case []any:
		if len(typed) == 0 {
			return true
		}
		for _, item := range typed {
			if !isEmptyCardValue(item) {
				return false
			}
		}
		return true
	case map[string]any:
		if len(typed) == 0 {
			return true
		}
		for _, item := range typed {
			if !isEmptyCardValue(item) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func coerceInt(value any) (int, bool) {
	switch number := value.(type) {
	case int:
		return number, true
	case int32:
		return int(number), true
	case int64:
		return int(number), true
	case float64:
		return int(number), true
	default:
		return 0, false
	}
}

func coerceNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case float32:
		return float64(number), true
	case float64:
		return number, true
	case int:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	default:
		return 0, false
	}
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func arraySchema(description string, items map[string]any) map[string]any {
	return map[string]any{
		"type":        "array",
		"description": description,
		"items":       items,
	}
}

func stringSchema(description string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
	}
}

func boolSchema(description string) map[string]any {
	return map[string]any{
		"type":        "boolean",
		"description": description,
	}
}

func numberSchema(description string) map[string]any {
	return map[string]any{
		"type":        "number",
		"description": description,
	}
}

func intEnumSchema(description string, values ...int) map[string]any {
	return map[string]any{
		"type":        "integer",
		"enum":        values,
		"description": description,
	}
}

func enumStringSchema(description string, values ...string) map[string]any {
	return map[string]any{
		"type":        "string",
		"enum":        values,
		"description": description,
	}
}

func selectionItemSchema(description string) map[string]any {
	schema := objectSchema(
		map[string]any{
			"question_key": stringSchema("Required selection question key."),
			"title":        stringSchema("Optional selection title."),
			"disable":      boolSchema("Optional disable flag."),
			"selected_id":  stringSchema("Optional default selected option ID."),
			"option_list": arraySchema(
				"Selection options, 1-10 items.",
				objectSchema(
					map[string]any{
						"id":   stringSchema("Required option ID."),
						"text": stringSchema("Required option text."),
					},
					"id",
					"text",
				),
			),
		},
		"question_key",
		"option_list",
	)
	schema["description"] = description
	return schema
}
