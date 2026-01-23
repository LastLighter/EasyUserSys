package email

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

var (
	ErrEmailNotConfigured = errors.New("email service not configured")
	ErrSendFailed         = errors.New("failed to send email")
)

// ResendClient Resend 邮件服务客户端
// API Key 是共享的，fromEmail 根据 system_code 动态获取
type ResendClient struct {
	apiKey string
}

// NewResendClient 创建新的 Resend 客户端
func NewResendClient(apiKey string) *ResendClient {
	return &ResendClient{
		apiKey: apiKey,
	}
}

// IsConfigured 检查邮件服务 API Key 是否已配置
func (c *ResendClient) IsConfigured() bool {
	return c.apiKey != ""
}

// sendEmailRequest Resend API 请求结构
type sendEmailRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
}

// sendEmailResponse Resend API 响应结构
type sendEmailResponse struct {
	ID string `json:"id"`
}

// SendEmail 发送邮件
// fromEmail: 发件人邮箱（根据 system_code 动态获取）
func (c *ResendClient) SendEmail(fromEmail, to, subject, htmlContent string) error {
	if !c.IsConfigured() {
		return ErrEmailNotConfigured
	}
	if fromEmail == "" {
		return ErrEmailNotConfigured
	}

	reqBody := sendEmailRequest{
		From:    fromEmail,
		To:      []string{to},
		Subject: subject,
		HTML:    htmlContent,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("%w: status code %d", ErrSendFailed, resp.StatusCode)
	}

	return nil
}

// SendVerificationCode 发送验证码邮件
// fromEmail: 发件人邮箱（根据 system_code 动态获取）
func (c *ResendClient) SendVerificationCode(fromEmail, to, code, codeType string) error {
	var subject, title, description string

	switch codeType {
	case "signup":
		subject = "邮箱验证码 - 注册确认"
		title = "欢迎注册"
		description = "感谢您的注册！请使用以下验证码完成邮箱验证："
	case "reset_password":
		subject = "邮箱验证码 - 重置密码"
		title = "密码重置"
		description = "您正在重置密码，请使用以下验证码完成验证："
	default:
		subject = "邮箱验证码"
		title = "验证码"
		description = "请使用以下验证码完成验证："
	}

	htmlContent := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s</title>
</head>
<body style="margin: 0; padding: 0; font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; background-color: #f4f4f4;">
    <table role="presentation" style="width: 100%%; border-collapse: collapse;">
        <tr>
            <td align="center" style="padding: 40px 0;">
                <table role="presentation" style="width: 600px; border-collapse: collapse; background-color: #ffffff; border-radius: 8px; box-shadow: 0 2px 8px rgba(0,0,0,0.1);">
                    <tr>
                        <td style="padding: 40px 40px 20px 40px; text-align: center;">
                            <h1 style="margin: 0; color: #333333; font-size: 24px; font-weight: 600;">%s</h1>
                        </td>
                    </tr>
                    <tr>
                        <td style="padding: 0 40px 20px 40px; text-align: center;">
                            <p style="margin: 0; color: #666666; font-size: 16px; line-height: 1.5;">%s</p>
                        </td>
                    </tr>
                    <tr>
                        <td style="padding: 20px 40px; text-align: center;">
                            <div style="display: inline-block; background-color: #f8f9fa; border: 2px dashed #dee2e6; border-radius: 8px; padding: 20px 40px;">
                                <span style="font-size: 32px; font-weight: bold; letter-spacing: 8px; color: #007bff;">%s</span>
                            </div>
                        </td>
                    </tr>
                    <tr>
                        <td style="padding: 20px 40px 40px 40px; text-align: center;">
                            <p style="margin: 0; color: #999999; font-size: 14px;">验证码有效期为 10 分钟，请勿将验证码泄露给他人。</p>
                            <p style="margin: 10px 0 0 0; color: #999999; font-size: 14px;">如果您没有请求此验证码，请忽略此邮件。</p>
                        </td>
                    </tr>
                </table>
            </td>
        </tr>
    </table>
</body>
</html>
`, subject, title, description, code)

	return c.SendEmail(fromEmail, to, subject, htmlContent)
}
