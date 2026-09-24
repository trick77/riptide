import os

# Image builds stamp RIPTIDE_VERSION via Containerfile ARG so logs carry the
# real release tag instead of a hardcoded dev placeholder. OpenShift's BuildConfig
# (when used) sets OPENSHIFT_BUILD_COMMIT and takes precedence — that's the
# source-of-truth in cluster-built images.
__version__ = os.environ.get("OPENSHIFT_BUILD_COMMIT") or os.environ.get("RIPTIDE_VERSION") or "dev"
