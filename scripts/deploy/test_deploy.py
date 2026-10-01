import hashlib
import io
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import deploy


class ReleaseTests(unittest.TestCase):
    def test_replaces_only_requested_service_image(self):
        source = b'services:\r\n  app:\r\n    image: app:old\r\n    cpus: 1\r\n    environment:\r\n      VALUE: app:old\r\n  frontend:\r\n    image: web:old\r\n'
        changed = deploy.replace_images(source, {"app": "app:new"})
        self.assertEqual(source.replace(b"    image: app:old", b"    image: app:new"), changed)
        with self.assertRaises(ValueError):
            deploy.replace_images(source, {"missing": "app:new"})

    def test_rejects_escaping_source_paths(self):
        for name in ("../outside", "/etc/passwd", "nested/../../outside", "a\\b"):
            with self.subTest(name=name), self.assertRaises(ValueError):
                deploy.safe_relative(name)

    def test_failed_activation_restores_source_and_resource_limits(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "project"
            release = Path(directory) / "release"
            root.mkdir()
            release.mkdir()
            (root / "docker-compose.yml").write_text("services: {}\n", encoding="utf-8")
            config = b"services:\n  app:\n    image: app:old\n    mem_limit: 896m\n  frontend:\n    image: web:old\n"
            (root / "docker-compose.override.yml").write_bytes(config)
            original = "原始源码\r\n".encode("utf-8")
            (root / "old.go").write_bytes(original)
            files = []
            with tarfile.open(release / "source.tar.gz", "w:gz") as archive:
                for name in ("old.go", "new.go"):
                    content = "修改后的源码\n".encode("utf-8")
                    member = tarfile.TarInfo(name)
                    member.size = len(content)
                    archive.addfile(member, io.BytesIO(content))
                    files.append({"path": name, "before": hashlib.sha256(original).hexdigest() if name == "old.go" else None,
                                  "after": hashlib.sha256(content).hexdigest()})
            manifest = {"root": str(root), "images": {"app": "app:new"}, "files": files}
            def fail_first_start(_root, *args):
                if args[-1] == "app" and b"app:new" in (root / "docker-compose.override.yml").read_bytes():
                    raise RuntimeError("模拟启动失败")
            with patch.object(deploy, "compose", side_effect=fail_first_start), patch.object(deploy, "wait_healthy"):
                with self.assertRaisesRegex(RuntimeError, "模拟启动失败"):
                    deploy.activate(release, manifest)
            self.assertEqual(config, (root / "docker-compose.override.yml").read_bytes())
            self.assertEqual(original, (root / "old.go").read_bytes())
            self.assertFalse((root / "new.go").exists())


if __name__ == "__main__":
    unittest.main()
