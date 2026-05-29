"""BDD coverage for WeaveValidationError typed dispatch.

Round 136 SDK mirror of round-135 backend (admin handlers reject
malformed submissionCriteria with 400 InvalidParameter:submissionCriteria
carrying parameter+reason). Before round 136 the SDK surfaced these
as plain WeaveError — callers had to introspect ``err.error_name``
manually to distinguish a criteria-shape mistake from a generic
admin 400.

Pattern matches the round-118 WeaveVersionedLookupError pilot:
narrow-typed dispatch in _handle for one (status, errorName)
pair, with .parameter / .reason properties surfacing the structured
fields. Other 400 InvalidParameter:* responses (e.g.
:apiName, :displayName) STILL raise plain WeaveError so this
branch doesn't auto-capture every admin 400.

Acceptance criteria (Given → When → Then):

  Given an admin POST that returns 400 with
        errorName=InvalidParameter:submissionCriteria
  When  the SDK Client makes the call
  Then  it raises WeaveValidationError whose .parameter ==
        "submissionCriteria" and .reason carries the backend
        message

  Given the same status (400) but errorName=InvalidParameter:apiName
  When  the SDK Client makes the call
  Then  it raises plain WeaveError, NOT WeaveValidationError
        (the typed branch is narrow)

  Given a 400 without an InvalidParameter prefix (e.g.
        InvalidRequestBody)
  When  the SDK Client makes the call
  Then  it raises plain WeaveError

  Given the async client and the same 400 InvalidParameter:submissionCriteria
  When  the call is awaited
  Then  WeaveValidationError is raised with the same .parameter/.reason
        (async mirror)

  Given a WeaveValidationError where the parameters envelope omits
        the 'reason' field
  When  caller inspects .reason
  Then  the value defaults to '' (defensive — no KeyError)

  Given WeaveValidationError instance
  When  caller does isinstance(err, WeaveError)
  Then  True (it must remain a WeaveError subclass so existing
        catch-all handlers still work)

Tests written FIRST (RED) before adding the class + dispatch.
"""
from __future__ import annotations

import asyncio
import json

import httpx
import pytest
import respx

from weave_client import Client, WeaveAsyncClient, WeaveError, WeaveValidationError


SUBMISSION_CRITERIA_ERROR = {
    "errorCode": "INVALID_ARGUMENT",
    "errorName": "InvalidParameter:submissionCriteria",
    "errorInstanceId": "abc-123",
    "parameters": {
        "parameter": "submissionCriteria",
        "reason": "unknown submission criteria type: \"weirdType\"",
    },
}

API_NAME_ERROR = {
    "errorCode": "INVALID_ARGUMENT",
    "errorName": "InvalidParameter:apiName",
    "errorInstanceId": "def-456",
    "parameters": {"parameter": "apiName", "reason": "apiName is required"},
}

REQUEST_BODY_ERROR = {
    "errorCode": "INVALID_ARGUMENT",
    "errorName": "InvalidRequestBody",
    "errorInstanceId": "ghi-789",
    "parameters": {"reason": "invalid JSON"},
}


class TestSyncValidationDispatch:
    @respx.mock
    def test_400_criteria_raises_validation_error(self):
        """Given a 400 InvalidParameter:submissionCriteria response,
        when the SDK invokes a request that triggers it, then the
        client raises WeaveValidationError carrying parameter +
        reason."""
        respx.post("http://wv/api/admin/ontologies/o/actionTypes").mock(
            return_value=httpx.Response(400, json=SUBMISSION_CRITERIA_ERROR)
        )
        c = Client("http://wv", access_token="t")

        with pytest.raises(WeaveValidationError) as excinfo:
            c._request(
                "POST",
                "/api/admin/ontologies/o/actionTypes",
                json_body={"apiName": "x", "submissionCriteria": {"type": "weirdType"}},
            )

        err = excinfo.value
        assert err.status_code == 400
        assert err.parameter == "submissionCriteria"
        assert "weirdType" in err.reason

    @respx.mock
    def test_400_other_invalid_parameter_falls_through_to_plain_weave_error(self):
        """Given a 400 InvalidParameter:apiName (different errorName),
        when the SDK invokes a request that triggers it, then it
        raises plain WeaveError, NOT WeaveValidationError. The typed
        branch must be narrow so future 400s don't get auto-captured."""
        respx.post("http://wv/api/admin/ontologies/o/actionTypes").mock(
            return_value=httpx.Response(400, json=API_NAME_ERROR)
        )
        c = Client("http://wv", access_token="t")

        with pytest.raises(WeaveError) as excinfo:
            c._request(
                "POST",
                "/api/admin/ontologies/o/actionTypes",
                json_body={"apiName": ""},
            )

        assert not isinstance(excinfo.value, WeaveValidationError)
        assert excinfo.value.error_name == "InvalidParameter:apiName"

    @respx.mock
    def test_400_invalid_request_body_falls_through(self):
        """Other 400 errorNames (not InvalidParameter:*) also fall
        through to plain WeaveError."""
        respx.post("http://wv/api/admin/ontologies/o/actionTypes").mock(
            return_value=httpx.Response(400, json=REQUEST_BODY_ERROR)
        )
        c = Client("http://wv", access_token="t")

        with pytest.raises(WeaveError) as excinfo:
            c._request(
                "POST", "/api/admin/ontologies/o/actionTypes", json_body={}
            )

        assert not isinstance(excinfo.value, WeaveValidationError)

    @respx.mock
    def test_validation_error_reason_defaults_to_empty(self):
        """When the backend omits the 'reason' field (defensive),
        .reason returns '' rather than raising KeyError."""
        envelope = dict(SUBMISSION_CRITERIA_ERROR)
        envelope["parameters"] = {"parameter": "submissionCriteria"}  # no reason
        respx.post("http://wv/api/admin/ontologies/o/actionTypes").mock(
            return_value=httpx.Response(400, json=envelope)
        )
        c = Client("http://wv", access_token="t")

        with pytest.raises(WeaveValidationError) as excinfo:
            c._request(
                "POST", "/api/admin/ontologies/o/actionTypes", json_body={}
            )
        assert excinfo.value.reason == ""

    def test_validation_error_is_weave_error_subclass(self):
        """isinstance(err, WeaveError) must remain True so existing
        catch-all `except WeaveError` blocks still trap it."""
        err = WeaveValidationError(
            400,
            error_name="InvalidParameter:submissionCriteria",
            parameters={"parameter": "submissionCriteria", "reason": "x"},
            raw_body=json.dumps(SUBMISSION_CRITERIA_ERROR),
        )
        assert isinstance(err, WeaveError)


class TestAsyncValidationDispatch:
    @respx.mock
    def test_async_400_criteria_raises_validation_error(self):
        """Async mirror — WeaveAsyncClient must raise the same typed
        exception on the same backend response shape."""
        respx.post("http://wv/api/admin/ontologies/o/actionTypes").mock(
            return_value=httpx.Response(400, json=SUBMISSION_CRITERIA_ERROR)
        )

        async def run():
            async with WeaveAsyncClient("http://wv", access_token="t") as c:
                with pytest.raises(WeaveValidationError) as excinfo:
                    await c._request(
                        "POST",
                        "/api/admin/ontologies/o/actionTypes",
                        json_body={"apiName": "x"},
                    )
                err = excinfo.value
                assert err.parameter == "submissionCriteria"
                assert "weirdType" in err.reason

        asyncio.run(run())
