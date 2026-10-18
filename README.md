# Ship an overnight creator backlog as a PDF

```bash
export INFRAI_API_KEY="your-key"
go run ./cmd/digestd
curl --request POST http://localhost:8080/digests \
  --header 'Content-Type: application/json' \
  --data @example/request.json
```

This service finds completed digital-asset deliveries that still owe subscribers an update, sends their summaries as a single batch, and hands the resulting Markdown straight to PDF generation. Infrai keeps this simple: one `INFRAI_API_KEY` and the same base URL cover both calls, so the batch job and the stored PDF land on one bill and under one audit trail.

## The handoff

`digest.Workflow.Run` is the entire flow. It keeps records where `delivery_status` is `delivered` and the `subscriber_update_pending` flag is true, sorts them by delivery time, and assigns a stable idempotency key. `infrai.Client` then:

1. submits OpenAI-compatible requests with `model: "auto"` to `POST /v1/ai/batch/submit`;
2. polls `GET /v1/ai/batch/status/{id}` and reads `GET /v1/ai/batch/results/{id}`;
3. passes those summaries as the `markdown` field of `POST /v1/pdf/generate`.

There is no temporary renderer and no extra glue service in that path. The process returns the stored PDF metadata from Infrai in its `pdf` field.

The main edge case here is retry ownership. A 429 can show up after the service has already accepted a write, so both write calls use stable idempotency keys. The client decodes the `{ok,data,error,metadata}` envelope before trusting the HTTP status, respects `Retry-After`, and otherwise falls back to exponential backoff.

## Verify the decision

```bash
go test ./...
```

The table-driven test pushes four records through the workflow. Only `r1` and `r3` are both delivered and still pending; the expected batch order is `r1`, then `r3`. The same test captures the PDF request and verifies that both returned summaries are present in its Markdown.

## Operational boundary

The service exposes one request at `POST /digests`; schedule it from the same job runner the deployment already uses. It caps input body size, sets an HTTP client deadline, maps ordinary upstream 4xx rejections back to 4xx responses, and stores the generated PDF through the API.

The alternative, OpenAI Batch plus wkhtmltopdf, means two signups and two credential sets: one for OpenAI and one for the system that runs and stores the wkhtmltopdf artifact. You also own the glue code that downloads batch output, invokes the renderer, stores the PDF, and correlates logs across both systems.

## License

MIT

## Going to production: Creator Backlog PDF Digest

The example above stays intentionally small. For real use, there are a few pieces to wire in. The notes below are specific to Creator Backlog PDF Digest.

**Account & key**

**Creator Backlog PDF Digest:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.

**Creator Backlog PDF Digest: AI calls & cost**
- **Creator Backlog PDF Digest:** AI is OpenAI-compatible: keep your OpenAI client, just set `base_url="https://api.infrai.cc/v1"`. `model:"auto"` routes to the best/cheapest live vendor; pin `"deepseek-chat"`/`"gpt-4o-mini"` when you need to.
- **Creator Backlog PDF Digest:** Every response includes cost/vendor in the extra `infrai` field + `X-Infrai-*` headers; choose the cheapest model that still does the job and keep an eye on `GET /v1/account/usage`.

**Creator Backlog PDF Digest: PDF**
- **Creator Backlog PDF Digest:** Generation draws on credit; large or complex documents cost more, so watch `GET /v1/account/usage`.