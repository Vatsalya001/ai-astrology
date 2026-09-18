package charts

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// ─── GET /print/chart?token=… ────────────────────────────────────────

// Print serves one chart to a browser that holds a valid print token,
// and to nothing else.
//
// ── Why this route exists ──
//
// The PDF is produced by driving headless Chrome at a print-styled web
// route, so the document and the site can never drift apart. That
// browser is a subprocess on a worker with no session, so it needs some
// credential — and the narrow one is a single-use token scoped to one
// chart, minted by the worker that spawned it.
//
// ── The chart id comes from the TOKEN, never from the request ──
//
// This is the whole security property, and it is the obvious thing to
// get wrong. A handler that reads `?id=` and merely checks the token is
// valid has built an authorisation bypass: any user could mint a token
// for their own chart and then read anybody's by changing one query
// parameter. There is deliberately no chart id in this route's path or
// query at all, so that mistake is unavailable rather than merely
// avoided.
//
// ── Not behind the ownership middleware, nor behind `authenticate` ──
//
// `RequireProfileOwnership` resolves the caller from a session, and this
// caller has none. The token IS the authorisation, and redeeming it
// yields the user and profile the worker scoped it to.
//
// Which is why the route is mounted as a SIBLING of `/charts` rather
// than inside it: that subtree applies `authenticate` with `r.Use`, so
// anything registered within it is authenticated regardless of the
// comment above it. See router.go, and the test that asserts this route
// answers with no Authorization header at all.
func (h *Handler) Print(w http.ResponseWriter, r *http.Request) {
	if h.tokens == nil {
		// Misconfiguration, not a client error. The route should not be
		// mounted without a token store.
		h.writeErr(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
			"Something went wrong. Please try again.",
			errors.New("charts: print route mounted without a token store"))
		return
	}

	token := strings.TrimSpace(r.URL.Query().Get("token"))

	scope, err := h.tokens.Redeem(r.Context(), token)
	if err != nil {
		if errors.Is(err, ErrPrintTokenInvalid) {
			/*
			  404, not 401 or 403.

			  The same rule the rest of the product follows for
			  cross-user access: a 403 confirms the resource exists. Here
			  it would also confirm that a token was once real, which
			  turns a guessing attack into a probing one.
			*/
			h.writeErr(w, r, http.StatusNotFound, "NOT_FOUND", "Not found.", nil)
			return
		}
		h.handleError(w, r, err)
		return
	}

	bundle, err := h.svc.PrintBundle(r.Context(), scope.UserID, scope.ProfileID, h.now())
	if err != nil {
		h.handleError(w, r, err)
		return
	}

	/*
	  No-store, and it matters more here than anywhere else in the API.

	  The response is somebody's birth data, reached with a credential in
	  a query string, by a browser whose cache lives on the worker's
	  disk. A shared cache keyed on that URL would serve it again after
	  the token had been redeemed and deleted — defeating single-use
	  outside this process, where none of the Redis machinery can see it.
	*/
	w.Header().Set("Cache-Control", "no-store, private")
	writeJSON(w, http.StatusOK, bundle)
}

// ─── minting, for the worker ─────────────────────────────────────────

// MintPrintToken is called by the PDF worker, not over HTTP.
//
// Exported on the handler because the worker already holds one, and a
// second construction path for the same store is a second place for the
// TTL to drift.
func (h *Handler) MintPrintToken(
	r *http.Request,
	userID, profileID uuid.UUID,
) (string, error) {
	if h.tokens == nil {
		return "", errors.New("charts: no print token store configured")
	}
	return h.tokens.Mint(r.Context(), PrintScope{UserID: userID, ProfileID: profileID})
}
