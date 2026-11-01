package rules

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type FieldDef struct {
	Path        string   `json:"path"`
	Label       string   `json:"label"`
	Type        string   `json:"type"` // string,int,bool,select,textarea,intlist,stringlist,headers,json
	Required    bool     `json:"required,omitempty"`
	Options     []Option `json:"options,omitempty"`
	Default     any      `json:"default,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Help        string   `json:"help,omitempty"`
}

type TypeDef struct {
	TypeKind    string     `json:"typeKind"`
	Label       string     `json:"label"`
	Category    string     `json:"category"`
	Description string     `json:"description"`
	Scope       string     `json:"scope"`
	Fields      []FieldDef `json:"fields"`
	Kind        string     `json:"kind"` // filter | action
}

type Schema struct {
	Filters []TypeDef `json:"filters"`
	Actions []TypeDef `json:"actions"`
}

var stringOps = []Option{
	{Value: "Exact", Label: "等于 (Exact)"},
	{Value: "Contains", Label: "包含 (Contains)"},
	{Value: "StartsWith", Label: "开头是 (StartsWith)"},
	{Value: "EndsWith", Label: "结尾是 (EndsWith)"},
	{Value: "Regex", Label: "正则 (Regex)"},
}

func textFields(extra ...FieldDef) []FieldDef {
	base := []FieldDef{
		{Path: "pattern", Label: "匹配内容", Type: "string", Required: true, Placeholder: "例如 example.com 或 ^/api/"},
		{Path: "operation", Label: "匹配方式", Type: "select", Default: "Exact", Options: stringOps},
		{Path: "caseSensitive", Label: "区分大小写", Type: "bool", Default: false},
		{Path: "inverted", Label: "反向匹配（NOT）", Type: "bool", Default: false},
	}
	return append(base, extra...)
}

func filter(kind, label, category, scope, desc string, fields []FieldDef) TypeDef {
	return TypeDef{TypeKind: kind, Label: label, Category: category, Scope: scope, Description: desc, Fields: fields, Kind: "filter"}
}

func action(kind, label, category, scope, desc string, fields []FieldDef) TypeDef {
	return TypeDef{TypeKind: kind, Label: label, Category: category, Scope: scope, Description: desc, Fields: fields, Kind: "action"}
}

func onlyInverted() []FieldDef {
	return []FieldDef{{Path: "inverted", Label: "反向匹配（NOT）", Type: "bool", Default: false}}
}

func bodyTransformFields() []FieldDef {
	return []FieldDef{
		{Path: "operation", Label: "操作", Type: "select", Default: "Replace", Options: []Option{
			{Value: "Replace", Label: "文本替换"},
			{Value: "RegexReplace", Label: "正则替换"},
			{Value: "Set", Label: "设置固定内容"},
		}},
		{Path: "pattern", Label: "查找内容 / 正则", Type: "string", Placeholder: "仅 Replace / RegexReplace 使用"},
		{Path: "replacement", Label: "替换为", Type: "string", Placeholder: "正则替换支持 $1"},
		{Path: "content", Label: "固定内容", Type: "textarea", Placeholder: "仅 Set 使用，例如 JSON 文本"},
		{Path: "contentBase64", Label: "固定内容（Base64）", Type: "textarea", Help: "二进制 body 使用；优先于 content"},
		{Path: "fileName", Label: "从文件读取", Type: "string", Placeholder: "/config/mock/example.json", Help: "优先于 content / base64；文件在规则加载时读取"},
		{Path: "contentType", Label: "覆盖 Content-Type", Type: "string", Placeholder: "application/json"},
		{Path: "encodingName", Label: "文本编码", Type: "string", Default: "utf-8"},
		{Path: "caseSensitive", Label: "区分大小写", Type: "bool", Default: true},
		{Path: "maxBodyBytes", Label: "最大改写字节数", Type: "int", Default: 8388608, Help: "超过限制时保持原始 body，不中断请求"},
	}
}

func BuildSchema() Schema {
	filters := []TypeDef{
		filter("AnyFilter", "任意请求", "通用", "onAuthorityReceived", "匹配所有请求", onlyInverted()),
		filter("HostFilter", "域名", "URL", "onAuthorityReceived", "按主机名匹配（不含端口）", textFields()),
		filter("AuthorityFilter", "主机:端口", "URL", "onAuthorityReceived", "按 authority 匹配", textFields(
			FieldDef{Path: "port", Label: "端口", Type: "int", Placeholder: "留空表示任意端口"},
		)),
		filter("PathFilter", "URL 路径", "URL", "requestHeaderReceivedFromClient", "按路径匹配", textFields()),
		filter("AbsoluteUriFilter", "完整 URL", "URL", "requestHeaderReceivedFromClient", "按完整 URL 匹配", textFields()),
		filter("QueryStringFilter", "查询参数", "URL", "requestHeaderReceivedFromClient", "按查询参数匹配", textFields(
			FieldDef{Path: "name", Label: "参数名", Type: "string", Placeholder: "留空表示任意参数"},
		)),
		filter("MethodFilter", "HTTP 方法", "请求", "requestHeaderReceivedFromClient", "按请求方法匹配", []FieldDef{
			{Path: "pattern", Label: "方法", Type: "string", Required: true, Placeholder: "GET / POST / PUT ..."},
			{Path: "operation", Label: "匹配方式", Type: "select", Default: "Exact", Options: stringOps},
			{Path: "caseSensitive", Label: "区分大小写", Type: "bool", Default: false},
			{Path: "inverted", Label: "反向匹配（NOT）", Type: "bool", Default: false},
		}),
		filter("GetFilter", "GET 请求", "请求", "requestHeaderReceivedFromClient", "仅匹配 GET", onlyInverted()),
		filter("PostFilter", "POST 请求", "请求", "requestHeaderReceivedFromClient", "仅匹配 POST", onlyInverted()),
		filter("PutFilter", "PUT 请求", "请求", "requestHeaderReceivedFromClient", "仅匹配 PUT", onlyInverted()),
		filter("PatchFilter", "PATCH 请求", "请求", "requestHeaderReceivedFromClient", "仅匹配 PATCH", onlyInverted()),
		filter("DeleteFilter", "DELETE 请求", "请求", "requestHeaderReceivedFromClient", "仅匹配 DELETE", onlyInverted()),
		filter("RequestHeaderFilter", "请求头", "请求", "requestHeaderReceivedFromClient", "按请求头匹配", textFields(
			FieldDef{Path: "headerName", Label: "Header 名称", Type: "string", Required: true, Placeholder: "authorization"},
		)),
		filter("ResponseHeaderFilter", "响应头", "响应", "responseHeaderReceivedFromRemote", "按响应头匹配", textFields(
			FieldDef{Path: "headerName", Label: "Header 名称", Type: "string", Required: true, Placeholder: "content-type"},
		)),
		filter("StatusCodeFilter", "状态码", "响应", "responseHeaderReceivedFromRemote", "按一个或多个状态码匹配", []FieldDef{
			{Path: "statusCodes", Label: "状态码", Type: "intlist", Required: true, Placeholder: "200, 301, 404"},
			{Path: "inverted", Label: "反向匹配（NOT）", Type: "bool", Default: false},
		}),
		filter("StatusCodeSuccessFilter", "2xx 成功", "响应", "responseHeaderReceivedFromRemote", "匹配 2xx", onlyInverted()),
		filter("StatusCodeRedirectionFilter", "3xx 重定向", "响应", "responseHeaderReceivedFromRemote", "匹配 3xx", onlyInverted()),
		filter("StatusCodeClientErrorFilter", "4xx 客户端错误", "响应", "responseHeaderReceivedFromRemote", "匹配 4xx", onlyInverted()),
		filter("StatusCodeServerErrorFilter", "5xx 服务端错误", "响应", "responseHeaderReceivedFromRemote", "匹配 5xx", onlyInverted()),
		filter("JsonRequestFilter", "JSON 请求体", "内容类型", "requestBodyReceivedFromClient", "匹配 JSON 请求", onlyInverted()),
		filter("JsonResponseFilter", "JSON 响应体", "内容类型", "responseBodyReceivedFromRemote", "匹配 JSON 响应", onlyInverted()),
		filter("HtmlResponseFilter", "HTML 响应", "内容类型", "responseHeaderReceivedFromRemote", "匹配 HTML 响应", onlyInverted()),
		filter("ImageFilter", "图片响应", "内容类型", "responseHeaderReceivedFromRemote", "匹配图片响应", onlyInverted()),
		filter("CssStyleFilter", "CSS 响应", "内容类型", "responseHeaderReceivedFromRemote", "匹配 CSS 响应", onlyInverted()),
		filter("FontFilter", "字体响应", "内容类型", "responseHeaderReceivedFromRemote", "匹配字体响应", onlyInverted()),
		filter("ContentTypeXmlFilter", "XML 响应", "内容类型", "responseHeaderReceivedFromRemote", "匹配 XML 响应", onlyInverted()),
		filter("ProtobufFilter", "Protobuf 响应", "内容类型", "responseHeaderReceivedFromRemote", "匹配 protobuf", onlyInverted()),
		filter("IsGrpcFilter", "gRPC 请求", "内容类型", "requestHeaderReceivedFromClient", "匹配 gRPC", onlyInverted()),
		filter("HasRequestBodyFilter", "有请求体", "请求", "requestBodyReceivedFromClient", "请求体非空", onlyInverted()),
		filter("NetworkErrorFilter", "网络错误", "错误", "responseBodyReceivedFromRemote", "请求发生网络错误", onlyInverted()),
		filter("NoResponseFilter", "无响应", "错误", "responseBodyReceivedFromRemote", "没有收到响应", onlyInverted()),
		filter("IsSecureFilter", "HTTPS", "连接", "onAuthorityReceived", "TLS 连接", onlyInverted()),
		filter("IsWebSocketFilter", "WebSocket", "连接", "requestHeaderReceivedFromClient", "WebSocket 请求", onlyInverted()),
		filter("HasAuthorizationFilter", "带 Authorization", "请求", "requestHeaderReceivedFromClient", "存在 Authorization 头", onlyInverted()),
		filter("HasAuthorizationBearerFilter", "带 Bearer Token", "请求", "requestHeaderReceivedFromClient", "Authorization 为 Bearer", onlyInverted()),
		filter("HasCommentFilter", "有备注", "元数据", "responseBodyReceivedFromRemote", "请求带备注", onlyInverted()),
		filter("HasTagFilter", "有标签", "元数据", "responseBodyReceivedFromRemote", "请求带标签", onlyInverted()),
		filter("HasCookieOnRequestFilter", "请求带 Cookie", "Cookie", "requestHeaderReceivedFromClient", "按 Cookie 匹配", textFields(
			FieldDef{Path: "name", Label: "Cookie 名称", Type: "string", Placeholder: "留空表示任意 Cookie"},
		)),
		filter("HasSetCookieOnResponseFilter", "响应带 Set-Cookie", "Cookie", "responseHeaderReceivedFromRemote", "按 Set-Cookie 匹配", textFields(
			FieldDef{Path: "name", Label: "Cookie 名称", Type: "string", Placeholder: "留空表示任意 Cookie"},
		)),
		filter("FilterCollection", "条件组合 (AND/OR)", "通用", "onAuthorityReceived", "把多个 filter 组合成 AND / OR", []FieldDef{
			{Path: "operation", Label: "组合方式", Type: "select", Default: "And", Options: []Option{
				{Value: "And", Label: "全部满足 (AND)"},
				{Value: "Or", Label: "任一满足 (OR)"},
			}},
			{Path: "children", Label: "子条件 (JSON 数组)", Type: "json", Required: true, Help: "例如 [{\"typeKind\":\"HostFilter\",\"pattern\":\"example.com\"}]"},
		}),
	}

	actions := []TypeDef{
		action("AddRequestHeaderAction", "添加请求头", "请求头", "requestHeaderReceivedFromClient", "向请求追加一个 Header", []FieldDef{
			{Path: "headerName", Label: "Header 名称", Type: "string", Required: true},
			{Path: "headerValue", Label: "Header 值", Type: "string", Required: true},
		}),
		action("UpdateRequestHeaderAction", "修改请求头", "请求头", "requestHeaderReceivedFromClient", "修改请求头，可选择不存在时添加", []FieldDef{
			{Path: "headerName", Label: "Header 名称", Type: "string", Required: true},
			{Path: "headerValue", Label: "Header 值", Type: "string", Required: true},
			{Path: "addIfMissing", Label: "不存在时添加", Type: "bool", Default: true},
			{Path: "appendSeparator", Label: "追加分隔符", Type: "string", Placeholder: "例如 ,"},
		}),
		action("DeleteRequestHeaderAction", "删除请求头", "请求头", "requestHeaderReceivedFromClient", "删除指定请求头", []FieldDef{
			{Path: "headerName", Label: "Header 名称", Type: "string", Required: true},
		}),
		action("AddResponseHeaderAction", "添加响应头", "响应头", "responseHeaderReceivedFromRemote", "向响应追加一个 Header", []FieldDef{
			{Path: "headerName", Label: "Header 名称", Type: "string", Required: true},
			{Path: "headerValue", Label: "Header 值", Type: "string", Required: true},
		}),
		action("UpdateResponseHeaderAction", "修改响应头", "响应头", "responseHeaderReceivedFromRemote", "修改响应头，可选择不存在时添加", []FieldDef{
			{Path: "headerName", Label: "Header 名称", Type: "string", Required: true},
			{Path: "headerValue", Label: "Header 值", Type: "string", Required: true},
			{Path: "addIfMissing", Label: "不存在时添加", Type: "bool", Default: true},
		}),
		action("DeleteResponseHeaderAction", "删除响应头", "响应头", "responseHeaderReceivedFromRemote", "删除指定响应头", []FieldDef{
			{Path: "headerName", Label: "Header 名称", Type: "string", Required: true},
		}),
		action("ConsoleTransformRequestBodyAction", "改写请求体", "请求体", "requestHeaderReceivedFromClient", "按路径/域名匹配后改写发往上游的请求体", bodyTransformFields()),
		action("ConsoleTransformResponseBodyAction", "改写响应体", "响应体", "responseHeaderReceivedFromRemote", "按路径/域名匹配后改写上游返回的响应体", bodyTransformFields()),
		action("MockedResponseAction", "Mock 响应", "响应体", "requestHeaderReceivedFromClient", "直接返回自定义响应，不访问上游", []FieldDef{
			{Path: "response.statusCode", Label: "状态码", Type: "int", Default: 200},
			{Path: "response.headers", Label: "响应头", Type: "headers", Help: "例如 Content-Type: application/json"},
			{Path: "response.body.origin", Label: "Body 来源", Type: "select", Default: "FromString", Options: []Option{
				{Value: "FromString", Label: "直接填写文本"},
				{Value: "FromFile", Label: "从文件读取"},
				{Value: "FromImmediateArray", Label: "Base64"},
			}},
			{Path: "response.body.type", Label: "Body 类型", Type: "select", Default: "Json", Options: []Option{
				{Value: "Json", Label: "JSON"},
				{Value: "Text", Label: "文本"},
				{Value: "Html", Label: "HTML"},
				{Value: "Xml", Label: "XML"},
				{Value: "Css", Label: "CSS"},
				{Value: "JavaScript", Label: "JavaScript"},
				{Value: "Binary", Label: "二进制"},
			}},
			{Path: "response.body.text", Label: "响应内容", Type: "textarea"},
			{Path: "response.body.fileName", Label: "文件路径", Type: "string", Placeholder: "/config/mock/example.json"},
			{Path: "response.body.contentBase64", Label: "Base64 内容", Type: "textarea"},
		}),
		action("AbortAction", "中断连接", "拦截", "requestHeaderReceivedFromClient", "直接中断请求", nil),
		action("RejectWithStatusCodeAction", "返回状态码", "拦截", "requestHeaderReceivedFromClient", "返回指定 HTTP 状态码", []FieldDef{
			{Path: "statusCode", Label: "状态码", Type: "int", Default: 403},
		}),
		action("RejectWithMessageAction", "返回自定义错误", "拦截", "requestHeaderReceivedFromClient", "返回状态码和消息体", []FieldDef{
			{Path: "statusCode", Label: "状态码", Type: "int", Default: 403},
			{Path: "message", Label: "消息内容", Type: "textarea", Default: "Request blocked by proxy"},
			{Path: "contentType", Label: "Content-Type", Type: "string", Default: "text/plain"},
		}),
		action("ForwardAction", "转发到新 URL", "转发", "requestHeaderReceivedFromClient", "把请求转发到另一个 URL", []FieldDef{
			{Path: "url", Label: "目标 URL", Type: "string", Required: true, Placeholder: "https://example.com"},
		}),
		action("ChangeRequestMethodAction", "修改请求方法", "请求", "requestHeaderReceivedFromClient", "改变 HTTP 方法", []FieldDef{
			{Path: "newMethod", Label: "新方法", Type: "string", Required: true, Placeholder: "GET"},
		}),
		action("ChangeRequestPathAction", "修改请求路径", "请求", "requestHeaderReceivedFromClient", "改变请求路径", []FieldDef{
			{Path: "newPath", Label: "新路径", Type: "string", Required: true, Placeholder: "/v2/api"},
		}),
		action("InjectHtmlTagAction", "注入 HTML", "响应体", "responseHeaderReceivedFromRemote", "在指定标签后注入 HTML", []FieldDef{
			{Path: "tag", Label: "标签", Type: "string", Default: "head", Placeholder: "head / body"},
			{Path: "htmlContent", Label: "注入内容", Type: "textarea", Placeholder: "<script>...</script>"},
			{Path: "fromFile", Label: "从文件读取", Type: "bool", Default: false},
			{Path: "fileName", Label: "文件路径", Type: "string", Placeholder: "/config/inject.html"},
			{Path: "encoding", Label: "编码", Type: "string", Default: "utf8"},
			{Path: "restrictToHtml", Label: "仅 HTML 响应", Type: "bool", Default: true},
		}),
		action("ApplyCommentAction", "添加备注", "元数据", "requestHeaderReceivedFromClient", "给匹配的请求加备注", []FieldDef{
			{Path: "comment", Label: "备注", Type: "string", Required: true},
		}),
		action("ApplyTagAction", "添加标签", "元数据", "requestHeaderReceivedFromClient", "给匹配的请求加标签", []FieldDef{
			{Path: "tag.value", Label: "标签值", Type: "string", Required: true, Placeholder: "例如 login-api"},
			{Path: "tag.identifier", Label: "标签 ID", Type: "string", Help: "留空自动生成 UUID"},
		}),
		action("DelayAction", "延迟", "流量整形", "responseHeaderReceivedFromRemote", "延迟请求/响应", []FieldDef{
			{Path: "duration", Label: "延迟毫秒", Type: "int", Default: 1000},
			{Path: "scope", Label: "生效时机", Type: "select", Default: "ResponseHeaderReceivedFromRemote", Options: []Option{
				{Value: "OnAuthorityReceived", Label: "解析目标域名后"},
				{Value: "RequestHeaderReceivedFromClient", Label: "收到请求头后"},
				{Value: "ResponseHeaderReceivedFromRemote", Label: "收到响应头后"},
				{Value: "ResponseBodyReceivedFromRemote", Label: "收到响应体后"},
			}},
		}),
		action("ServeDirectoryAction", "返回静态目录", "响应体", "requestHeaderReceivedFromClient", "从本地目录返回文件", []FieldDef{
			{Path: "directory", Label: "目录路径", Type: "string", Required: true, Placeholder: "/config/static"},
		}),
		action("SetRequestCookieAction", "设置请求 Cookie", "Cookie", "requestHeaderReceivedFromClient", "添加/覆盖请求 Cookie", []FieldDef{
			{Path: "name", Label: "名称", Type: "string", Required: true},
			{Path: "value", Label: "值", Type: "string", Required: true},
		}),
		action("SetResponseCookieAction", "设置响应 Cookie", "Cookie", "responseHeaderReceivedFromRemote", "添加响应 Set-Cookie", []FieldDef{
			{Path: "name", Label: "名称", Type: "string", Required: true},
			{Path: "value", Label: "值", Type: "string", Required: true},
			{Path: "path", Label: "Path", Type: "string", Default: "/"},
			{Path: "domain", Label: "Domain", Type: "string"},
			{Path: "expireInSeconds", Label: "过期秒数", Type: "int"},
			{Path: "maxAge", Label: "Max-Age", Type: "int"},
			{Path: "httpOnly", Label: "HttpOnly", Type: "bool", Default: false},
			{Path: "secure", Label: "Secure", Type: "bool", Default: false},
			{Path: "sameSite", Label: "SameSite", Type: "string", Placeholder: "Lax / Strict / None"},
		}),
		action("RemoveResponseCookieAction", "删除响应 Cookie", "Cookie", "responseHeaderReceivedFromRemote", "删除 Set-Cookie", []FieldDef{
			{Path: "name", Label: "名称", Type: "string", Required: true},
		}),
		action("SetUserAgentAction", "修改 User-Agent", "请求头", "requestHeaderReceivedFromClient", "设置请求 User-Agent", []FieldDef{
			{Path: "name", Label: "User-Agent", Type: "string", Required: true},
		}),
		action("ForceRemotePortAction", "强制上游端口", "转发", "onAuthorityReceived", "把上游连接指向指定端口", []FieldDef{
			{Path: "port", Label: "端口", Type: "int", Required: true},
		}),
		action("SetVariableAction", "设置变量", "变量", "requestHeaderReceivedFromClient", "把值存入会话变量，供后续规则使用", []FieldDef{
			{Path: "name", Label: "变量名", Type: "string", Required: true},
			{Path: "value", Label: "值", Type: "string", Required: true, Help: "支持 ${...} 变量语法"},
			{Path: "scope", Label: "作用域", Type: "string", Default: "RequestHeaderReceivedFromClient"},
		}),
		action("UpStreamProxyAction", "指定上游代理", "转发", "onAuthorityReceived", "通过另一个代理转发", []FieldDef{
			{Path: "host", Label: "代理主机", Type: "string", Required: true},
			{Path: "port", Label: "代理端口", Type: "int", Required: true},
			{Path: "proxyAuthorizationHeader", Label: "Proxy-Authorization", Type: "string"},
			{Path: "byPassHosts", Label: "直连主机", Type: "stringlist"},
		}),
		action("UseDnsOverHttpsAction", "使用 DoH", "DNS", "onAuthorityReceived", "通过 DoH 解析域名", []FieldDef{
			{Path: "nameOrUrl", Label: "DoH 名称或 URL", Type: "string", Required: true},
			{Path: "noCapture", Label: "不记录", Type: "bool", Default: false},
		}),
		action("NoOpAction", "空操作", "调试", "requestHeaderReceivedFromClient", "不做任何事，用于占位", nil),
	}

	return Schema{Filters: filters, Actions: actions}
}

func (s Schema) FindFilter(kind string) (TypeDef, bool) {
	for _, d := range s.Filters {
		if d.TypeKind == kind {
			return d, true
		}
	}
	return TypeDef{}, false
}

func (s Schema) FindAction(kind string) (TypeDef, bool) {
	for _, d := range s.Actions {
		if d.TypeKind == kind {
			return d, true
		}
	}
	return TypeDef{}, false
}
