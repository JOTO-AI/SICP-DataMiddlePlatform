"""Deterministic, fictional source fixtures; no network or third-party dependencies."""

from .source import MockSource, load_case

__all__ = ["MockSource", "load_case"]
