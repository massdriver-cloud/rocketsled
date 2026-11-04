import type { Environment } from "../types";
import { Pill } from "./ui";

// Track shows every package in an environment as a car riding the rail. A sled
// runs the length of the track while something is provisioning.
export function Track({ environment, live, appComponent }: { environment: Environment; live: boolean; appComponent?: string }) {
  return (
    <div className="track" aria-label="Packages">
      <div className="track__cars">
        {(environment.instances ?? []).map((pi) => (
          <div key={pi.id} className={`car car--${pi.status}`}>
            <div className="car__slug">
              {pi.componentSlug}
              {pi.componentSlug === appComponent && (
                <span className="eyebrow eyebrow--red" style={{ marginLeft: 8 }}>
                  app
                </span>
              )}
            </div>
            <div className="car__bundle" title={`${pi.bundleName}@${pi.version}`}>
              {pi.bundleName}@{pi.deployedVersion ?? pi.version}
            </div>
            <div>
              <Pill status={pi.status} />
            </div>
          </div>
        ))}
      </div>
      <div className="track__rails" aria-hidden="true" />
      {live && <div className="sled" aria-hidden="true" />}
    </div>
  );
}
