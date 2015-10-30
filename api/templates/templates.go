// Package templates interacts with the SparkPost Templates API.
// https://www.sparkpost.com/api#/reference/templates
package templates

import (
	"encoding/json"
	"fmt"
	"reflect"
	re "regexp"
	"strings"
	"time"

	"github.com/SparkPost/go-sparkpost/api"
)

// Templates is your handle for the Templates API.
type Templates struct{ api.API }

// New gets a Templates object ready to use with the specified config.
func New(cfg api.Config) (*Templates, error) {
	t := &Templates{}
	path := fmt.Sprintf("/api/v%d/templates", cfg.ApiVersion)
	err := t.Init(cfg, path)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// Template is the JSON structure accepted by and returned from the SparkPost Templates API.
// It's mostly metadata at this level - see Content and Options for more detail.
type Template struct {
	ID          string    `json:"id,omitempty"`
	Content     Content   `json:"content,omitempty"`
	Published   bool      `json:"published,omitempty"`
	Name        string    `json:"name,omitempty"`
	Description string    `json:"description,omitempty"`
	LastUse     time.Time `json:"last_use,omitempty"`
	LastUpdate  time.Time `json:"last_update_time,omitempty"`
	Options     *Options  `json:"options,omitempty"`
}

// Content is what you'll send to your Recipients.
// Knowledge of SparkPost's substitution/templating capabilities will come in handy here.
// https://www.sparkpost.com/api#/introduction/substitutions-reference
type Content struct {
	HTML         string            `json:"html,omitempty"`
	Text         string            `json:"text,omitempty"`
	Subject      string            `json:"subject,omitempty"`
	From         interface{}       `json:"from,omitempty"`
	ReplyTo      string            `json:"reply_to,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	EmailRFC822  string            `json:"email_rfc822,omitempty"`
	Attachments  []Attachment      `json:"attachments,omitempty"`
	InlineImages []InlineImage     `json:"inline_images,omitempty"`
}

// Attachment contains metadata and the contents of the file to attach.
type Attachment struct {
	MIMEType string `json:"type"`
	Filename string `json:"name"`
	B64Data  string `json:"data"`
}

// InlineImage contains metadata and the contents of the image to make available for inline use.
type InlineImage Attachment

// From describes the nested object way of specifying the From header.
// Content.From can be specified this way, or as a plain string.
type From struct {
	Email string
	Name  string
}

// Options specifies settings to apply to this Template.
// These settings may be overridden in the Transmission API call.
type Options struct {
	OpenTracking  string `json:"open_tracking,omitempty"`
	ClickTracking string `json:"click_tracking,omitempty"`
	Transactional string `json:"transactional,omitempty"`
}

// ParseFrom parses the various allowable Content.From values.
func ParseFrom(from interface{}) (f From, err error) {
	// handle the allowed types
	switch fromVal := from.(type) {
	case string: // simple string value
		if fromVal == "" {
			err = fmt.Errorf("Content.From may not be empty")
		} else {
			f.Email = fromVal
		}

	case map[string]interface{}:
		// auto-parsed nested json object
		for k, v := range fromVal {
			switch vVal := v.(type) {
			case string:
				if strings.EqualFold(k, "name") {
					f.Name = vVal
				} else if strings.EqualFold(k, "email") {
					f.Email = vVal
				}
			default:
				err = fmt.Errorf("strings are required for all Content.From values")
				break
			}
		}

	case map[string]string:
		// user-provided json literal (convenience)
		for k, v := range fromVal {
			if strings.EqualFold(k, "name") {
				f.Name = v
			} else if strings.EqualFold(k, "email") {
				f.Email = v
			}
		}

	default:
		err = fmt.Errorf("unsupported Content.From value type [%s]", reflect.TypeOf(fromVal))
	}

	return
}

// Validate runs sanity checks on a Template struct.
// This should catch most errors before attempting a doomed API call.
func (t *Template) Validate() error {
	if t == nil {
		return fmt.Errorf("Can't Validate a nil Template")
	}

	if t.Content.EmailRFC822 != "" {
		// TODO: optionally validate MIME structure
		// if MIME content is present, clobber all other Content options
		t.Content = Content{EmailRFC822: t.Content.EmailRFC822}
		return nil
	}

	// enforce required parameters
	if t.Content.Subject == "" {
		return fmt.Errorf("Template requires a non-empty Content.Subject")
	} else if t.Content.HTML == "" && t.Content.Text == "" {
		return fmt.Errorf("Template requires either Content.HTML or Content.Text")
	}
	_, err := ParseFrom(t.Content.From)
	if err != nil {
		return err
	}

	if len(t.Content.Attachments) > 0 {
		for _, att := range t.Content.Attachments {
			if len(att.Filename) > 255 {
				return fmt.Errorf("Attachment name length must be <= 255: [%s]", att.Filename)
			} else if strings.ContainsAny(att.B64Data, "\r\n") {
				return fmt.Errorf("Attachment data may not contain line breaks [\\r\\n]")
			}
		}
	}

	if len(t.Content.InlineImages) > 0 {
		for _, img := range t.Content.InlineImages {
			if len(img.Filename) > 255 {
				return fmt.Errorf("InlineImage name length must be <= 255: [%s]", img.Filename)
			} else if strings.ContainsAny(img.B64Data, "\r\n") {
				return fmt.Errorf("InlineImage data may not contain line breaks [\\r\\n]")
			}
		}
	}

	// enforce max lengths
	if len(t.ID) > 64 {
		return fmt.Errorf("Template id may not be longer than 64 bytes")
	} else if len(t.Name) > 1024 {
		return fmt.Errorf("Template name may not be longer than 1024 bytes")
	} else if len(t.Description) > 1024 {
		return fmt.Errorf("Template description may not be longer than 1024 bytes")
	}

	return nil
}

// SetHeaders is a convenience method which sets Template.Content.Headers to the provided map.
func (t *Template) SetHeaders(headers map[string]string) {
	t.Content.Headers = headers
}

// Create accepts a populated Template object, validates its Contents,
// and performs an API call against the configured endpoint.
func (t *Templates) Create(template *Template) (id string, res *api.Response, err error) {
	if template == nil {
		err = fmt.Errorf("Create called with nil Template")
		return
	}

	err = template.Validate()
	if err != nil {
		return
	}

	jsonBytes, err := json.Marshal(template)
	if err != nil {
		return
	}

	url := fmt.Sprintf("%s%s", t.Config.BaseUrl, t.Path)
	res, err = t.HttpPost(url, jsonBytes)
	if err != nil {
		return
	}

	if err = res.AssertJson(); err != nil {
		return
	}

	err = res.ParseResponse()
	if err != nil {
		return
	}

	if res.HTTP.StatusCode == 200 {
		var ok bool
		id, ok = res.Results["id"].(string)
		if !ok {
			err = fmt.Errorf("Unexpected response to Template creation")
		}

	} else if len(res.Errors) > 0 {
		// handle common errors
		err = res.PrettyError("Template", "create")
		if err != nil {
			return
		}

		if res.HTTP.StatusCode == 422 { // template syntax error
			eobj := res.Errors[0]
			err = fmt.Errorf("%s: %s\n%s", eobj.Code, eobj.Message, eobj.Description)
		} else { // everything else
			err = fmt.Errorf("%d: %s", res.HTTP.StatusCode, string(res.Body))
		}
	}

	return
}

// List returns metadata for all Templates in the system.
func (t *Templates) List() ([]Template, *api.Response, error) {
	url := fmt.Sprintf("%s%s", t.Config.BaseUrl, t.Path)
	res, err := t.HttpGet(url)
	if err != nil {
		return nil, nil, err
	}

	if err = res.AssertJson(); err != nil {
		return nil, res, err
	}

	if res.HTTP.StatusCode == 200 {
		var body []byte
		body, err = res.ReadBody()
		if err != nil {
			return nil, res, err
		}
		tlist := map[string][]Template{}
		if err = json.Unmarshal(body, &tlist); err != nil {
			return nil, res, err
		} else if list, ok := tlist["results"]; ok {
			return list, res, nil
		}
		return nil, res, fmt.Errorf("Unexpected response to Template list")

	} else {
		err = res.ParseResponse()
		if err != nil {
			return nil, res, err
		}
		if len(res.Errors) > 0 {
			err = res.PrettyError("Transmission", "list")
			if err != nil {
				return nil, res, err
			}
		}
		return nil, res, fmt.Errorf("%d: %s", res.HTTP.StatusCode, string(res.Body))
	}

	return nil, res, err
}

var nonDigit *re.Regexp = re.MustCompile(`\D`)

// Delete removes the Template with the specified id.
func (t *Templates) Delete(id string) (res *api.Response, err error) {
	if id == "" {
		err = fmt.Errorf("Delete called with blank id")
		return
	}
	if nonDigit.MatchString(id) {
		err = fmt.Errorf("Templates.Delete: id may only contain digits")
		return
	}

	url := fmt.Sprintf("%s%s/%s", t.Config.BaseUrl, t.Path, id)
	res, err = t.HttpDelete(url)
	if err != nil {
		return
	}

	if err = res.AssertJson(); err != nil {
		return
	}

	err = res.ParseResponse()
	if err != nil {
		return
	}

	if res.HTTP.StatusCode == 200 {
		return

	} else if len(res.Errors) > 0 {
		// handle common errors
		err = res.PrettyError("Template", "delete")
		if err != nil {
			return
		}

		// handle template-specific ones
		if res.HTTP.StatusCode == 409 {
			err = fmt.Errorf("Template with id [%s] is in use by msg generation", id)
		} else { // everything else
			err = fmt.Errorf("%d: %s", res.HTTP.StatusCode, string(res.Body))
		}
	}

	return
}
