"""Tests for check.py: the repository files pass and seeded violations fail."""
import copy
import json
import unittest

import check


def policy(*actions, effect="Allow", resource="*"):
    return {"Version": "2012-10-17", "Statement": [{"Sid": "S", "Effect": effect, "Action": list(actions), "Resource": resource}]}


class ReadOnly(unittest.TestCase):
    def setUp(self):
        self.good = json.loads(check.API_POLICY.read_text())

    def test_repo_policy_passes(self):
        self.assertEqual(check.check_readonly_policy(self.good), [])

    def test_read_on_wildcard_resource_passes(self):
        doc = copy.deepcopy(self.good)
        doc["Statement"][0]["Resource"] = "*"
        self.assertEqual(check.check_readonly_policy(doc), [])

    def test_seeded_write_fails(self):
        doc = copy.deepcopy(self.good)
        doc["Statement"][1]["Action"].append("rds:ModifyDBInstance")
        findings = check.check_readonly_policy(doc)
        self.assertTrue(any("rds:ModifyDBInstance" in f for f in findings), findings)

    def test_service_wildcard_fails(self):
        for bad in ("*", "rds:*", "s3:PutObject", "cloudwatch:PutMetricData", "elasticbeanstalk:UpdateEnvironment"):
            doc = copy.deepcopy(self.good)
            doc["Statement"][0]["Action"].append(bad)
            self.assertTrue(check.check_readonly_policy(doc), bad)

    def test_not_action_and_deny_fail(self):
        doc = copy.deepcopy(self.good)
        doc["Statement"].append({"Effect": "Allow", "NotAction": "iam:*", "Resource": "*"})
        self.assertTrue(any("NotAction" in f for f in check.check_readonly_policy(doc)))
        deny = policy(*check.NAMED_READS, effect="Deny")
        self.assertTrue(check.check_readonly_policy(deny))

    def test_other_reads_pass(self):
        doc = policy(*check.NAMED_READS, "ec2:DescribeInstances", "s3:GetObject", "logs:GetLogEvents", "iam:ListRoles")
        self.assertEqual(check.check_readonly_policy(doc), [])

    def test_lookalike_verbs_fail(self):
        for bad in ("s3:GetObject/../Put", "ec2:Describe_Instances", "iam:GenerateCredentialReport", "ec2:Start*"):
            doc = policy(*check.NAMED_READS, bad)
            self.assertTrue(check.check_readonly_policy(doc), bad)

    def test_missing_named_read_fails(self):
        doc = policy("cloudwatch:GetMetricData")
        self.assertTrue(check.check_readonly_policy(doc))


class Worker(unittest.TestCase):
    def setUp(self):
        self.good = json.loads(check.WORKER_POLICY.read_text())

    def test_repo_policy_passes(self):
        self.assertEqual(check.check_worker_policy(self.good), [])

    def test_wrong_namespace_fails(self):
        doc = copy.deepcopy(self.good)
        doc["Statement"][0]["Condition"]["StringEquals"]["cloudwatch:namespace"] = "AWS/EC2"
        self.assertTrue(check.check_worker_policy(doc))

    def test_missing_condition_fails(self):
        doc = copy.deepcopy(self.good)
        del doc["Statement"][0]["Condition"]
        self.assertTrue(check.check_worker_policy(doc))

    def test_extra_action_fails(self):
        doc = copy.deepcopy(self.good)
        doc["Statement"][0]["Action"] = ["cloudwatch:PutMetricData", "cloudwatch:PutMetricAlarm"]
        self.assertTrue(check.check_worker_policy(doc))

    def test_extra_statement_fails(self):
        doc = copy.deepcopy(self.good)
        doc["Statement"].append(copy.deepcopy(doc["Statement"][0]))
        self.assertTrue(check.check_worker_policy(doc))


class Timings(unittest.TestCase):
    def setUp(self):
        self.text = check.TIMINGS.read_text()
        self.main = check.STACK_MAIN.read_text()

    def test_repo_passes(self):
        self.assertEqual(check.check_timings(self.text, self.main), [])

    def test_each_drift_fails(self):
        for key, want in check.EXPECTED.items():
            bad = self.text.replace(f"{key} = {want}", f"{key} = {want + 1}", 1)
            if bad == self.text:  # aligned `=` in the file
                bad = __import__("re").sub(rf"({key}\s*=\s*){want}\b", rf"\g<1>{want + 1}", self.text, count=1)
            self.assertNotEqual(bad, self.text, key)
            self.assertTrue(check.check_timings(bad), key)

    def test_unwired_value_fails(self):
        main = self.main.replace("local.deregistration_delay_seconds", "75", 1)
        self.assertTrue(check.check_timings(self.text, main))


if __name__ == "__main__":
    unittest.main()
