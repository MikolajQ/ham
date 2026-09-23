import React from 'react';
import clsx from 'clsx';
import styles from './styles.module.css';

const FeatureList = [
  {
    title: 'Easy to Use',
    Svg: require('@site/static/img/easy.svg').default,
    description: (
      <>
	 Ham was made easy to use and user-friendly, it is very easy to use and hard to abuse,
	 it has the most sane defaults to prevent economical loss for the user.
      </>
    ),
  },
  {
    title: 'Cross Platform',
    Svg: require('@site/static/img/cross_platform.svg').default,
    description: (
      <>
	 Ham client program can on multiple platforms and architectures including <b>Android
	    itself using Termux Terminal Emulator</b>.
      </>
    ),
  },
  {
    title: 'Hackable',
    Svg: require('@site/static/img/hackable.svg').default,
    description: (
      <>
	 Ham uses a YAML recipe file held in a git repository, each device and ROM can have it's
	 own repo with different recipes, different branches can add or remove elements that are
	 built directly into the ROM.
      </>
    ),
  },
  {
    title: 'Modern',
    Svg: require('@site/static/img/modern.svg').default,
    description: (
      <>
	 Ham YAML files are losely based on Github Actions YAML file, and are designed to keep everything
	 modern, make things easy for the developers.
      </>
    ),
  },
  {
    title: 'Super Fast',
    Svg: require('@site/static/img/fast.svg').default,
    description: (
      <>
	 The recipe runs directly on the Hetzner VM (Ubuntu 24.04), without Docker or LXC.
      </>
    ),
  },
  {
    title: 'Billed by the hour',
    Svg: require('@site/static/img/cheap.svg').default,
    description: (
      <>
	 The build machine is a CCX33 (8 vCPU, 32 GB RAM, zram) plus a 400 GB volume.
	 In Germany that server is about <b>€0.22 per hour</b> before VAT (Hetzner list of 15 June 2026),
	 and the volume is a few tens of cents for a build. Both are deleted when the job finishes.
      </>
    ),
  },
];

function Feature({Svg, title, description}) {
  return (
    <div className={clsx('col col--4')}>
      <div className="text--center">
        <Svg className={styles.featureSvg} role="img" />
      </div>
      <div className="text--center padding-horiz--md">
        <h3>{title}</h3>
        <p>{description}</p>
      </div>
    </div>
  );
}

export default function HomepageFeatures() {
  return (
    <section className={styles.features}>
      <div className="container">
        <div className="row">
          {FeatureList.map((props, idx) => (
            <Feature key={idx} {...props} />
          ))}
        </div>
      </div>
    </section>
  );
}
