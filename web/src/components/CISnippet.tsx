import { Eyebrow } from "./ui";

export function CISnippet({ stackId }: { stackId: string }) {
  const base = window.location.origin;
  return (
    <div className="card">
      <Eyebrow>Deploy from CI after the image is pushed</Eyebrow>
      <p className="muted" style={{ margin: "6px 0 12px", fontSize: 14 }}>
        Instances following the given branch redeploy with the tag you send. Omit imageTag to use the commit sha.
      </p>
      <pre className="snippet">{`curl -sS -X POST ${base}/ci/v1/stacks/${stackId}/deploys \\
  -H "Authorization: Bearer $ROCKETSLED_DEPLOY_TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{"ref":"'"$GITHUB_REF_NAME"'","sha":"'"$GITHUB_SHA"'","imageTag":"'"$GITHUB_SHA"'"}'`}</pre>
    </div>
  );
}
