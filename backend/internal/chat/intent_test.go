package chat

import "testing"

func TestClassifyIntent_Email(t *testing.T) {
	r := ClassifyIntent("why was baptista.manuel@rain.co.za declined yesterday?")
	if r.Intent != IntentCustomerLookup {
		t.Errorf("expected customer_lookup, got %s", r.Intent)
	}
	if r.Args["email"] != "baptista.manuel@rain.co.za" {
		t.Errorf("expected email arg, got %v", r.Args)
	}
	if r.Confidence < 0.9 {
		t.Errorf("email should be high confidence, got %v", r.Confidence)
	}
}

func TestClassifyIntent_MSISDN(t *testing.T) {
	cases := []string{
		"look up 0721234567",
		"who has 084 123 4567 right now",
		"customer with +27 82 123 4567",
	}
	for _, p := range cases {
		r := ClassifyIntent(p)
		if r.Intent != IntentCustomerLookup {
			t.Errorf("%q: expected customer_lookup, got %s", p, r.Intent)
			continue
		}
		if r.Args["msisdn"] == "" {
			t.Errorf("%q: missing msisdn arg, got %v", p, r.Args)
		}
	}
}

func TestClassifyIntent_IMSI(t *testing.T) {
	r := ClassifyIntent("debug 655380004807362 please")
	if r.Intent != IntentCustomerLookup {
		t.Errorf("expected customer_lookup, got %s", r.Intent)
	}
	if r.Args["imsi"] != "655380004807362" {
		t.Errorf("expected imsi arg, got %v", r.Args)
	}
}

func TestClassifyIntent_UUID(t *testing.T) {
	r := ClassifyIntent("show me 7c0e1d92-19f5-44ce-a4a9-37cc8e44f5ad")
	if r.Intent != IntentCustomerLookup {
		t.Errorf("expected customer_lookup, got %s", r.Intent)
	}
	if r.Args["customer_id"] == "" {
		t.Errorf("expected customer_id arg, got %v", r.Args)
	}
}

func TestClassifyIntent_SystemStatus(t *testing.T) {
	cases := []string{
		"is axiom up?",
		"any alerts firing right now?",
		"system status please",
		"is gaussdb down?",
	}
	for _, p := range cases {
		r := ClassifyIntent(p)
		if r.Intent != IntentSystemStatus {
			t.Errorf("%q: expected system_status, got %s", p, r.Intent)
		}
	}
}

func TestClassifyIntent_CodeTask(t *testing.T) {
	cases := []string{
		"fix the dashboard uptime bug",
		"refactor the IMSI lookup",
		"add a function to format msisdn",
		"open a PR for these changes",
		"write a test for the cascade",
	}
	for _, p := range cases {
		r := ClassifyIntent(p)
		if r.Intent != IntentCodeTask {
			t.Errorf("%q: expected code_task, got %s", p, r.Intent)
		}
		if r.Confidence < 0.6 {
			t.Errorf("%q: code task should be confident, got %v", p, r.Confidence)
		}
	}
}

func TestClassifyIntent_DataQuery(t *testing.T) {
	cases := []string{
		"how many payments failed yesterday",
		"top 10 customers by ltv",
		"average decline rate this month",
	}
	for _, p := range cases {
		r := ClassifyIntent(p)
		if r.Intent != IntentDataQuery {
			t.Errorf("%q: expected data_query, got %s", p, r.Intent)
		}
	}
}

func TestClassifyIntent_CustomerKeywordWithoutID(t *testing.T) {
	// Customer lookup where the user said "this customer" without
	// pasting an identifier. Should land on customer_lookup but at
	// reduced confidence so the handler can ask for the identifier.
	r := ClassifyIntent("why did this customer churn")
	if r.Intent != IntentCustomerLookup {
		t.Errorf("expected customer_lookup, got %s", r.Intent)
	}
	if r.Confidence > 0.6 {
		t.Errorf("expected reduced confidence (no ID), got %v", r.Confidence)
	}
}

func TestClassifyIntent_Unclear(t *testing.T) {
	cases := []string{
		"hello",
		"thanks",
		"random gibberish blah blah",
	}
	for _, p := range cases {
		r := ClassifyIntent(p)
		if r.Intent != IntentUnclear {
			t.Errorf("%q: expected unclear, got %s (%s)", p, r.Intent, r.Reason)
		}
	}
}

func TestClassifyIntent_EmptyPrompt(t *testing.T) {
	r := ClassifyIntent("")
	if r.Intent != IntentUnclear {
		t.Errorf("expected unclear on empty, got %s", r.Intent)
	}
	if r.Confidence != 0 {
		t.Errorf("empty prompt confidence must be 0, got %v", r.Confidence)
	}

	r = ClassifyIntent("   \t \n")
	if r.Intent != IntentUnclear {
		t.Errorf("expected unclear on whitespace, got %s", r.Intent)
	}
}

func TestClassifyIntent_EmailWinsOverCodeKeyword(t *testing.T) {
	// "Implement" is a code-task keyword; the email should still win
	// because the user is anchoring the question on a specific
	// customer. Phase A2 may decide to compose both — for now we
	// route to customer-lookup and let downstream sort it out.
	r := ClassifyIntent("implement a fix for alice@example.com")
	if r.Intent != IntentCustomerLookup {
		t.Errorf("expected customer_lookup, got %s", r.Intent)
	}
}

func TestStripNonDigits(t *testing.T) {
	cases := map[string]string{
		"084 123 4567":   "0841234567",
		"+27-82-123-4567": "27821234567",
		"abc123def":      "123",
		"":               "",
	}
	for in, want := range cases {
		if got := stripNonDigits(in); got != want {
			t.Errorf("stripNonDigits(%q)=%q, want %q", in, got, want)
		}
	}
}
