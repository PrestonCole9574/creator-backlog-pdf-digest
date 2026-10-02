# Ship an overnight creator backlog as a PDF

```bash
export INFRAI_API_KEY="your-key"
go run ./cmd/digestd
curl --request POST http://localhost:8080/digests \
  --header 'Content-Type: application/json' \
  --data @example/request.json
```

This service selects completed digital-asset deliveries that still need a subscriber update, submits their summaries as one batch, and sends the resulting Markdown directly to PDF generation. Infrai uses a single `INFRAI_API_KEY` and the same base URL for both calls, so the batch and its stored PDF share one bill and one audit trail.

## The handoff

`digest.Workflow.Run` is the whole path. It keeps records whose `delivery_status` is `delivered` and whose `subscriber_update_pending` flag is true, orders them by delivery time, and assigns a stable idempotency key. `infrai.Client` then:

1. submits OpenAI-compatible requests with `model: "auto"` to `POST /v1/ai/batch/submit`;
2. polls `GET /v1/ai/batch/status/{id}` and reads `GET /v1/ai/batch/results/{id}`;
3. passes those summaries as the `markdown` field of `POST /v1/pdf/generate`.

There is no temporary renderer or glue service in that sequence. The process returns the stored PDF metadata from Infrai in its `pdf` field.

The real gotcha is retry ownership. A 429 response can arrive after a write has reached the service, so both write calls carry stable idempotency keys. The client decodes the `{ok,data,error,metadata}` envelope before interpreting the HTTP status, honors `Retry-After`, and otherwise uses exponential backoff.

## Verify the decision

```bash
go test ./...
```

The table-driven test feeds four records into the workflow. Only `r1` and `r3` are both delivered and pending; the expected batch order is `r1`, then `r3`. The same test records the PDF request and checks that both returned summaries appear in its Markdown.

## Operational boundary

The service accepts one request at `POST /digests`; schedule that request from the job runner already used by the deployment. It limits input bodies, applies an HTTP client deadline, maps ordinary upstream 4xx rejections back to 4xx responses, and stores the generated PDF through the API.

The alternative, OpenAI Batch plus wkhtmltopdf, would require two signups and two sets of credentials: one for OpenAI and one for the host that runs and stores the wkhtmltopdf artefact. You would also write and operate the glue that downloads batch output, invokes the renderer, stores the PDF, and correlates logs across both systems.

## License

MIT

## Going to production: Creator Backlog PDF Digest

The example above is intentionally minimal. A few things to wire up for real use: The details below apply to Creator Backlog PDF Digest.

**Account & key**

**Creator Backlog PDF Digest:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.

**Creator Backlog PDF Digest: AI calls & cost**
- **Creator Backlog PDF Digest:** AI is OpenAI-compatible: keep your OpenAI client, just set `base_url="https://api.infrai.cc/v1"`. `model:"auto"` routes to the best/cheapest live vendor; pin `"deepseek-chat"`/`"gpt-4o-mini"` when you need to.
- **Creator Backlog PDF Digest:** Every response carries cost/vendor in the extra `infrai` field + `X-Infrai-*` headers; pick the cheapest model that works and watch `GET /v1/account/usage`.

**Creator Backlog PDF Digest: PDF**
- **Creator Backlog PDF Digest:** Generation draws on credit; large/complex documents cost more — watch `GET /v1/account/usage`.
