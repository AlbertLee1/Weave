"""BDD acceptance tests for the round-45 AttachmentsAPI (PRD-V2 Gap-D2 close-out)."""
from __future__ import annotations

import json
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Any, Callable, Dict, List, Tuple

from weave_client import Attachment, Client


# Binary-aware stub server. The base test_client.py _StubHandler
# decodes request bodies as UTF-8 (which corrupts binary uploads)
# and always responds with Content-Type: application/json. Round 45
# needs both binary requests AND binary responses with arbitrary
# Content-Type, so we ship a dedicated stub here.

# Route value: (status, body_bytes, content_type)
BinaryRoute = Tuple[int, bytes, str]


class _BinaryStubHandler(BaseHTTPRequestHandler):
    routes: Dict[str, BinaryRoute] = {}
    requests: List[Dict[str, Any]] = []

    def log_message(self, format, *args):  # silence test output
        return

    def _record(self, body: bytes) -> None:
        type(self).requests.append({
            "method": self.command,
            "path": self.path,
            "auth": self.headers.get("Authorization", ""),
            "content_type": self.headers.get("Content-Type", ""),
            "body_bytes": body,
        })

    def _serve(self) -> None:
        length = int(self.headers.get("Content-Length", "0") or 0)
        body = self.rfile.read(length) if length else b""
        self._record(body)
        # Strip query string from key — matches the JSON stub helper.
        key = f"{self.command} {self.path.split('?', 1)[0]}"
        if key in type(self).routes:
            status, payload, content_type = type(self).routes[key]
            self.send_response(status)
            self.send_header("Content-Type", content_type)
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            return
        self.send_response(404)
        self.send_header("Content-Type", "application/json")
        msg = b'{"errorCode":"NOT_FOUND","errorName":"NoStub","errorInstanceId":"x","parameters":{}}'
        self.send_header("Content-Length", str(len(msg)))
        self.end_headers()
        self.wfile.write(msg)

    do_GET = _serve
    do_POST = _serve
    do_PUT = _serve
    do_DELETE = _serve


class _BinaryStubServer:
    def __init__(self, routes: Dict[str, BinaryRoute]):
        _BinaryStubHandler.routes = routes
        _BinaryStubHandler.requests = []
        self.server = HTTPServer(("127.0.0.1", 0), _BinaryStubHandler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def __enter__(self) -> "_BinaryStubServer":
        self.thread.start()
        return self

    def __exit__(self, *exc):
        self.server.shutdown()
        self.server.server_close()

    @property
    def url(self) -> str:
        host, port = self.server.server_address
        return f"http://{host}:{port}"

    @property
    def requests(self) -> List[Dict[str, Any]]:
        return _BinaryStubHandler.requests


# Tiny helper: build a JSON route as (status, body_bytes,
# content_type) so each test can mix JSON metadata responses with
# binary content responses.
def _json_route(status: int, payload: Dict[str, Any]) -> BinaryRoute:
    return (status, json.dumps(payload).encode("utf-8"), "application/json")


class AttachmentsGlobalEndpointTests(unittest.TestCase):
    """Global attachment store: upload + get_metadata + get_content."""

    def test_upload_sends_binary_body_with_content_type_and_filename(self):
        att_payload = {
            "rid": "ri.attachments.main.attachment.abc",
            "filename": "incident.pdf",
            "sizeBytes": 9,
            "mediaType": "application/pdf",
            "createdAt": "2026-04-01T00:00:00Z",
            "linked": False,
        }
        routes = {
            "POST /api/v2/ontologies/attachments/upload": _json_route(200, att_payload),
        }
        with _BinaryStubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            att = c.attachments.upload(
                filename="incident.pdf",
                content=b"%PDF-...\x00",  # 9 raw bytes including NUL
                media_type="application/pdf",
            )
        self.assertIsInstance(att, Attachment)
        self.assertEqual(att.rid, "ri.attachments.main.attachment.abc")
        self.assertEqual(att.size_bytes, 9)
        sent = srv.requests[0]
        # Server must have received the raw bytes verbatim (no UTF-8
        # mangling) and the caller-supplied Content-Type.
        self.assertEqual(sent["body_bytes"], b"%PDF-...\x00")
        self.assertEqual(sent["content_type"], "application/pdf")
        self.assertIn("filename=incident.pdf", sent["path"])

    def test_upload_with_rid_uses_path_segment(self):
        att_payload = {
            "rid": "ri.attachments.main.attachment.fixed-id",
            "filename": "f.bin",
            "sizeBytes": 4,
            "mediaType": "application/octet-stream",
            "createdAt": "2026-04-01T00:00:00Z",
            "linked": False,
        }
        routes = {
            "POST /api/v2/ontologies/attachments/upload/ri.attachments.main.attachment.fixed-id":
                _json_route(200, att_payload),
        }
        with _BinaryStubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            att = c.attachments.upload_with_rid(
                "ri.attachments.main.attachment.fixed-id",
                filename="f.bin",
                content=b"\x00\x01\x02\x03",
            )
        self.assertEqual(att.rid, "ri.attachments.main.attachment.fixed-id")
        sent = srv.requests[0]
        self.assertEqual(sent["body_bytes"], b"\x00\x01\x02\x03")
        # Default media_type when caller omits the kwarg.
        self.assertEqual(sent["content_type"], "application/octet-stream")

    def test_get_metadata_parses_attachment_model(self):
        att_payload = {
            "rid": "ri.attachments.main.attachment.x",
            "filename": "report.csv",
            "sizeBytes": 42,
            "mediaType": "text/csv",
            "createdAt": "2026-04-01T00:00:00Z",
            "linked": True,
        }
        routes = {
            "GET /api/v2/ontologies/attachments/ri.attachments.main.attachment.x":
                _json_route(200, att_payload),
        }
        with _BinaryStubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            att = c.attachments.get_metadata("ri.attachments.main.attachment.x")
        self.assertEqual(att.media_type, "text/csv")
        self.assertTrue(att.linked)

    def test_get_content_returns_raw_bytes(self):
        # Binary payload with embedded NUL and a non-UTF-8 byte
        # sequence to confirm the SDK doesn't try to decode it.
        raw = b"\x00\x01\xff\xfe\xfdGIF89a"
        routes = {
            "GET /api/v2/ontologies/attachments/ri.attachments.main.attachment.x/content":
                (200, raw, "image/gif"),
        }
        with _BinaryStubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            blob = c.attachments.get_content("ri.attachments.main.attachment.x")
        self.assertEqual(blob, raw)


class AttachmentsPropertyEndpointTests(unittest.TestCase):
    """Object-property addressing: get_property_metadata + content,
    plus the _by_rid variants."""

    def test_get_property_metadata_uses_object_path(self):
        att_payload = {
            "rid": "ri.attachments.main.attachment.linked",
            "filename": "evidence.png",
            "sizeBytes": 100,
            "mediaType": "image/png",
            "createdAt": "2026-04-01T00:00:00Z",
            "linked": True,
        }
        routes = {
            "GET /api/v2/ontologies/nw/objects/Incident/INC-001/attachments/attachedReport":
                _json_route(200, att_payload),
        }
        with _BinaryStubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            att = c.attachments.get_property_metadata(
                "nw", "Incident", "INC-001", "attachedReport",
            )
        self.assertEqual(att.rid, "ri.attachments.main.attachment.linked")
        self.assertEqual(att.filename, "evidence.png")

    def test_get_property_content_returns_raw_bytes(self):
        raw = b"\x89PNG\r\n\x1a\n..."
        routes = {
            "GET /api/v2/ontologies/nw/objects/Incident/INC-001/attachments/attachedReport/content":
                (200, raw, "image/png"),
        }
        with _BinaryStubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            blob = c.attachments.get_property_content(
                "nw", "Incident", "INC-001", "attachedReport",
            )
        self.assertEqual(blob, raw)

    def test_get_property_metadata_by_rid_addresses_specific_attachment(self):
        att_payload = {
            "rid": "ri.attachments.main.attachment.specific",
            "filename": "v2.png",
            "sizeBytes": 200,
            "mediaType": "image/png",
            "createdAt": "2026-04-02T00:00:00Z",
            "linked": True,
        }
        routes = {
            "GET /api/v2/ontologies/nw/objects/Incident/INC-001/attachments/photos/ri.attachments.main.attachment.specific":
                _json_route(200, att_payload),
        }
        with _BinaryStubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            att = c.attachments.get_property_metadata_by_rid(
                "nw", "Incident", "INC-001", "photos",
                "ri.attachments.main.attachment.specific",
            )
        self.assertEqual(att.rid, "ri.attachments.main.attachment.specific")

    def test_get_property_content_by_rid_returns_raw_bytes(self):
        raw = b"GIF89a..."
        routes = {
            "GET /api/v2/ontologies/nw/objects/Incident/INC-001/attachments/photos/ri.attachments.main.attachment.spec/content":
                (200, raw, "image/gif"),
        }
        with _BinaryStubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            blob = c.attachments.get_property_content_by_rid(
                "nw", "Incident", "INC-001", "photos",
                "ri.attachments.main.attachment.spec",
            )
        self.assertEqual(blob, raw)


# Keep `Callable` referenced so the typing import isn't pruned by
# a future linter pass.
_ = Callable[[int], int]


if __name__ == "__main__":
    unittest.main()
