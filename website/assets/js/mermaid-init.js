(() => {
  const initMermaid = async () => {
    if (!window.mermaid) {
      return;
    }

    const styles = getComputedStyle(document.documentElement);
    const value = (name) => styles.getPropertyValue(name).trim();

    window.mermaid.initialize({
      startOnLoad: false,
      securityLevel: "strict",
      theme: "base",
      fontFamily: getComputedStyle(document.body).fontFamily,
      themeVariables: {
        fontFamily: getComputedStyle(document.body).fontFamily,
        background: value("--paper-raised"),
        mainBkg: value("--paper-raised"),
        primaryColor: value("--mint-wash"),
        primaryTextColor: value("--text"),
        primaryBorderColor: value("--green-primary"),
        secondaryColor: value("--paper-deep"),
        secondaryTextColor: value("--text"),
        secondaryBorderColor: value("--mint"),
        tertiaryColor: value("--paper"),
        tertiaryTextColor: value("--text"),
        tertiaryBorderColor: value("--line"),
        lineColor: value("--green-primary"),
        textColor: value("--text"),
        actorBkg: value("--paper-raised"),
        actorBorder: value("--green-primary"),
        actorTextColor: value("--text"),
        noteBkgColor: value("--mint-wash"),
        noteBorderColor: value("--green-primary"),
        noteTextColor: value("--text"),
        clusterBkg: value("--paper-deep"),
        clusterBorder: value("--green-primary"),
        labelBoxBkgColor: value("--paper-raised"),
        labelBoxBorderColor: value("--green-primary"),
        labelTextColor: value("--text"),
        signalColor: value("--green-primary"),
        cScale0: value("--mint-wash"),
        cScale1: value("--yellow"),
        cScale2: value("--mint"),
        cScaleLabel0: value("--text"),
        cScaleLabel1: value("--text"),
        cScaleLabel2: value("--text")
      },
      flowchart: {
        htmlLabels: true,
        curve: "basis",
        useMaxWidth: true
      },
      sequence: {
        useMaxWidth: true,
        wrap: true
      }
    });

    const diagrams = Array.from(document.querySelectorAll(".mermaid"));
    for (const diagram of diagrams) {
      try {
        await window.mermaid.run({
          nodes: [diagram]
        });
      } catch (error) {
        diagram.dataset.mermaidError = "true";
        diagram.setAttribute("aria-label", "Diagram failed to render");
        console.error("Mermaid diagram failed to render", error);
      }
    }
  };

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", initMermaid, { once: true });
    return;
  }

  initMermaid();
})();
