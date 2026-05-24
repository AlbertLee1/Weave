"""AttachmentsAPI — read/write attachment blobs and inspect
attachment-property metadata via Foundry's
/api/v2/ontologies/attachments/* and
/api/v2/ontologies/{o}/objects/{type}/{pk}/attachments/{property}/*
surfaces.

PRD-V2 Gap-D2 close-out (round 45): the Go server has 8
attachment endpoints (4 global + 4 object-property) but the
Python SDK had none. Callers had to drop down to raw HTTP via
the internal Transport. This module wraps all 8 + adds a
Pydantic ``Attachment`` model.

Quickstart::

    from weave_client import Client

    client = Client("http://localhost:9117", access_token="...")

    # Upload a fresh blob.
    att = client.attachments.upload(
        filename="incident-report.pdf",
        content=Path("report.pdf").read_bytes(),
        media_type="application/pdf",
    )
    print(att.rid)  # ri.attachments.main.attachment.<uuid>

    # Inspect metadata + download bytes by RID.
    meta = client.attachments.get_metadata(att.rid)
    blob = client.attachments.get_content(att.rid)

    # Or address attachments via object property paths.
    att = client.attachments.get_property_metadata(
        "northwind", "Incident", "INC-001", "attachedReport",
    )
    blob = client.attachments.get_property_content(
        "northwind", "Incident", "INC-001", "attachedReport",
    )
"""
from __future__ import annotations

from typing import TYPE_CHECKING, Any, Optional

from ._http import quote_path
from .types import Attachment

if TYPE_CHECKING:
    from .client import Client


def _validate_attachment(payload: Any) -> Attachment:
    if hasattr(Attachment, "model_validate"):
        return Attachment.model_validate(payload or {})
    return Attachment(**(payload or {}))


class AttachmentsAPI:
    """Wraps ``/api/v2/ontologies/attachments/*`` (global) and
    ``/api/v2/ontologies/{o}/objects/{type}/{pk}/attachments/{property}/*``
    (object-property)."""

    def __init__(self, client: "Client"):
        self._client = client

    # ------------------------------------------------------------------
    # Global blob store — upload + read by RID
    # ------------------------------------------------------------------

    def upload(
        self,
        *,
        filename: str,
        content: bytes,
        media_type: str = "application/octet-stream",
    ) -> Attachment:
        """Upload a fresh blob; server mints the RID.

        ``filename`` is required by the server (returns 400
        MissingFilename otherwise). ``media_type`` is stamped
        verbatim onto the persisted ``Attachment.media_type`` and
        echoed back via the GetContent Content-Type header.
        """
        path = f"/api/v2/ontologies/attachments/upload?filename={quote_path(filename)}"
        resp = self._client._upload_binary(
            "POST", path, body=content, content_type=media_type,
        )
        return _validate_attachment(resp)

    def upload_with_rid(
        self,
        rid: str,
        *,
        filename: str,
        content: bytes,
        media_type: str = "application/octet-stream",
    ) -> Attachment:
        """Upload a blob under a caller-chosen RID.

        Used for idempotent re-uploads / rehydration paths where
        the caller has already persisted the RID elsewhere (e.g.
        in an action_log row) and wants to land the blob at the
        same identity.
        """
        path = (
            f"/api/v2/ontologies/attachments/upload/{quote_path(rid)}"
            f"?filename={quote_path(filename)}"
        )
        resp = self._client._upload_binary(
            "POST", path, body=content, content_type=media_type,
        )
        return _validate_attachment(resp)

    def get_metadata(self, rid: str) -> Attachment:
        """Get attachment metadata by RID."""
        path = f"/api/v2/ontologies/attachments/{quote_path(rid)}"
        return _validate_attachment(self._client._request("GET", path))

    def get_content(self, rid: str) -> bytes:
        """Download the raw blob bytes by RID.

        The server sends back the bytes with the
        ``Content-Type`` header set to whatever was supplied at
        upload time (defaults to ``application/octet-stream``).
        """
        path = f"/api/v2/ontologies/attachments/{quote_path(rid)}/content"
        return self._client._download_binary("GET", path)

    # ------------------------------------------------------------------
    # Object-property addressing — resolve via {ontology, type, pk, property}
    # ------------------------------------------------------------------

    def get_property_metadata(
        self,
        ontology: str,
        object_type: str,
        primary_key: str,
        property: str,
    ) -> Attachment:
        """Attachment metadata for the (single) RID stored in the
        named property of the given object.

        Returns 400 InvalidAttachmentProperty when the property
        holds an attachment array — use ``get_property_metadata_
        by_rid`` to address a specific array element.
        """
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/objects/{quote_path(object_type)}/{quote_path(primary_key)}"
            f"/attachments/{quote_path(property)}"
        )
        return _validate_attachment(self._client._request("GET", path))

    def get_property_content(
        self,
        ontology: str,
        object_type: str,
        primary_key: str,
        property: str,
    ) -> bytes:
        """Download the blob bytes addressed by an attachment
        property on the given object."""
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/objects/{quote_path(object_type)}/{quote_path(primary_key)}"
            f"/attachments/{quote_path(property)}/content"
        )
        return self._client._download_binary("GET", path)

    def get_property_metadata_by_rid(
        self,
        ontology: str,
        object_type: str,
        primary_key: str,
        property: str,
        attachment_rid: str,
    ) -> Attachment:
        """Attachment metadata addressed by ``attachment_rid`` within
        the named property's attachment array. 404
        AttachmentNotFound when the RID isn't present in the array.
        """
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/objects/{quote_path(object_type)}/{quote_path(primary_key)}"
            f"/attachments/{quote_path(property)}/{quote_path(attachment_rid)}"
        )
        return _validate_attachment(self._client._request("GET", path))

    def get_property_content_by_rid(
        self,
        ontology: str,
        object_type: str,
        primary_key: str,
        property: str,
        attachment_rid: str,
    ) -> bytes:
        """Download the blob bytes addressed by a specific RID
        inside an attachment array property."""
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/objects/{quote_path(object_type)}/{quote_path(primary_key)}"
            f"/attachments/{quote_path(property)}/{quote_path(attachment_rid)}/content"
        )
        return self._client._download_binary("GET", path)


__all__ = ["AttachmentsAPI"]
